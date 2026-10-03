import { createReadStream } from "node:fs";
import { appendFile, mkdir, open, readdir, readFile, rm, writeFile } from "node:fs/promises";
import { createInterface } from "node:readline";
import path from "node:path";
import { getSetting, setSetting } from "@/server/settings";
import { logActionLabel } from "@/shared/log-actions";

export const logLevels = ["info", "warning", "error"] as const;
export type LogLevel = (typeof logLevels)[number];
export type LogStatus = "success" | "failed";

export type LogSettings = {
  enabled: boolean;
  retentionDays: number;
  minLevel: LogLevel;
};

export type FileSyncLog = {
  id: string;
  connectionId: number;
  action: string;
  actionLabel?: string | null;
  target: string | null;
  level: LogLevel;
  detail: string | null;
  status: LogStatus;
  error: string | null;
  createdAt: string;
};

export type LogQueryInput = {
  connectionId?: number;
  limit: number;
  cursor?: string;
  levels?: LogLevel[];
  statuses?: LogStatus[];
  action?: string;
  target?: string;
  search?: string;
  dateFrom?: string;
  dateTo?: string;
};

export const defaultLogSettings: LogSettings = {
  enabled: true,
  retentionDays: 30,
  minLevel: "info",
};

const logFilePrefix = "s2a-manager-";
const logFileSuffix = ".log";
const logSettingsCacheTtlMs = 15_000;
const recentLogTailBytes = 2 * 1024 * 1024;
const maxRecentScanFiles = 7;
const maxFilteredScanFilesWithoutDate = 14;
let logSettingsCache: { expiresAt: number; value: LogSettings } = {
  expiresAt: 0,
  value: defaultLogSettings,
};
let hasLoadedLocalLogSettings = false;
let logSettingsRefreshPromise: Promise<LogSettings> | null = null;
let clearMarkersWriteQueue: Promise<void> = Promise.resolve();
const levelRank: Record<LogLevel, number> = {
  info: 0,
  warning: 1,
  error: 2,
};

function logsDir() {
  const configuredDir = process.env.S2A_LOG_DIR?.trim();
  if (configuredDir) return path.resolve(/*turbopackIgnore: true*/ configuredDir);
  return path.join(/*turbopackIgnore: true*/ process.cwd(), "logs");
}

function localDateKey(date = new Date()) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function logFileName(date = new Date()) {
  return `${logFilePrefix}${localDateKey(date)}${logFileSuffix}`;
}

function logFilePath(date = new Date()) {
  return path.join(/*turbopackIgnore: true*/ logsDir(), logFileName(date));
}

function logSettingsPath() {
  return path.join(/*turbopackIgnore: true*/ logsDir(), "settings.json");
}

function clearMarkersPath() {
  return path.join(/*turbopackIgnore: true*/ logsDir(), "cleared-connections.json");
}

function normalizeBoolean(value: string, fallback: boolean) {
  if (value === "true") return true;
  if (value === "false") return false;
  return fallback;
}

function normalizeRetentionDays(value: unknown) {
  const numeric = Number(value);
  if (!Number.isInteger(numeric) || numeric < 1) return defaultLogSettings.retentionDays;
  return Math.min(numeric, 3650);
}

export function normalizeLogLevel(value: unknown): LogLevel {
  return value === "warning" || value === "error" ? value : "info";
}

function normalizeLogStatus(value: unknown): LogStatus {
  return value === "failed" ? "failed" : "success";
}

export function levelFromStatus(status: LogStatus, requested?: LogLevel): LogLevel {
  if (requested) return requested;
  return status === "failed" ? "error" : "info";
}

export function shouldRecordLog(settings: LogSettings, level: LogLevel) {
  return settings.enabled && levelRank[level] >= levelRank[settings.minLevel];
}

function normalizeLogSettings(input: Partial<LogSettings> | null | undefined): LogSettings {
  return {
    enabled: input?.enabled ?? defaultLogSettings.enabled,
    retentionDays: normalizeRetentionDays(input?.retentionDays),
    minLevel: normalizeLogLevel(input?.minLevel),
  };
}

async function readLocalLogSettings() {
  try {
    const content = await readFile(/*turbopackIgnore: true*/ logSettingsPath(), "utf8");
    const parsed = JSON.parse(content) as Partial<LogSettings>;
    return normalizeLogSettings(parsed);
  } catch {
    return null;
  }
}

async function writeLocalLogSettings(settings: LogSettings) {
  await mkdir(/*turbopackIgnore: true*/ logsDir(), { recursive: true });
  await writeFile(/*turbopackIgnore: true*/ logSettingsPath(), `${JSON.stringify(settings, null, 2)}\n`, "utf8");
}

function normalizeClearMarkers(input: unknown) {
  const result: Record<string, string> = {};
  if (!input || typeof input !== "object") return result;

  for (const [key, value] of Object.entries(input)) {
    const connectionId = Number(key);
    if (!Number.isInteger(connectionId) || connectionId <= 0 || typeof value !== "string") continue;
    if (!safeParseDate(value)) continue;
    result[String(connectionId)] = value;
  }
  return result;
}

async function readClearMarkers() {
  try {
    const content = await readFile(/*turbopackIgnore: true*/ clearMarkersPath(), "utf8");
    return normalizeClearMarkers(JSON.parse(content));
  } catch {
    return {};
  }
}

async function writeClearMarkers(markers: Record<string, string>) {
  await mkdir(/*turbopackIgnore: true*/ logsDir(), { recursive: true });
  await writeFile(/*turbopackIgnore: true*/ clearMarkersPath(), `${JSON.stringify(markers, null, 2)}\n`, "utf8");
}

async function withClearMarkersLock<T>(work: () => Promise<T>) {
  const previous = clearMarkersWriteQueue;
  let release: () => void = () => undefined;
  clearMarkersWriteQueue = new Promise<void>((resolve) => {
    release = resolve;
  });

  await previous;
  try {
    return await work();
  } finally {
    release();
  }
}

export async function getLogSettings(): Promise<LogSettings> {
  const [enabled, retentionDays, minLevel] = await Promise.all([
    getSetting("logs_enabled", String(defaultLogSettings.enabled)),
    getSetting("logs_retention_days", String(defaultLogSettings.retentionDays)),
    getSetting("logs_min_level", defaultLogSettings.minLevel),
  ]);

  const settings = {
    enabled: normalizeBoolean(enabled, defaultLogSettings.enabled),
    retentionDays: normalizeRetentionDays(retentionDays),
    minLevel: normalizeLogLevel(minLevel),
  };
  logSettingsCache = { expiresAt: Date.now() + logSettingsCacheTtlMs, value: settings };
  await writeLocalLogSettings(settings).catch(() => undefined);
  return settings;
}

function refreshLogSettingsFromFileInBackground() {
  if (logSettingsCache.expiresAt > Date.now() || logSettingsRefreshPromise) return;
  logSettingsRefreshPromise = readLocalLogSettings()
    .then((settings) => {
      logSettingsCache = {
        expiresAt: Date.now() + logSettingsCacheTtlMs,
        value: settings ?? logSettingsCache.value,
      };
      return logSettingsCache.value;
    })
    .catch(() => {
      logSettingsCache = { expiresAt: Date.now() + logSettingsCacheTtlMs, value: logSettingsCache.value };
      return logSettingsCache.value;
    })
    .finally(() => {
      logSettingsRefreshPromise = null;
    });
}

async function getInitialLocalLogSettingsForWrite() {
  if (hasLoadedLocalLogSettings) return logSettingsCache.value;
  hasLoadedLocalLogSettings = true;
  const settings = await readLocalLogSettings();
  if (settings) {
    logSettingsCache = { expiresAt: Date.now() + logSettingsCacheTtlMs, value: settings };
    return logSettingsCache.value;
  }
  try {
    return await getLogSettings();
  } catch {
    logSettingsCache = { expiresAt: Date.now() + logSettingsCacheTtlMs, value: logSettingsCache.value };
  }
  return logSettingsCache.value;
}

async function getRetentionDaysForCleanup(retentionDays?: number) {
  if (retentionDays !== undefined) return normalizeRetentionDays(retentionDays);
  const settings = await readLocalLogSettings();
  return normalizeRetentionDays((settings ?? logSettingsCache.value).retentionDays);
}

async function getLogSettingsForWrite() {
  if (!hasLoadedLocalLogSettings) return getInitialLocalLogSettingsForWrite();
  refreshLogSettingsFromFileInBackground();
  return logSettingsCache.value;
}

export async function saveLogSettings(input: LogSettings) {
  const settings = {
    enabled: Boolean(input.enabled),
    retentionDays: normalizeRetentionDays(input.retentionDays),
    minLevel: normalizeLogLevel(input.minLevel),
  };

  await Promise.all([
    setSetting("logs_enabled", String(settings.enabled)),
    setSetting("logs_retention_days", String(settings.retentionDays)),
    setSetting("logs_min_level", settings.minLevel),
  ]);

  logSettingsCache = { expiresAt: Date.now() + logSettingsCacheTtlMs, value: settings };
  await writeLocalLogSettings(settings);
  return settings;
}

function serializeDetail(detail: unknown) {
  if (detail === undefined || detail === null) return null;
  if (typeof detail === "string") return detail;
  return JSON.stringify(detail);
}

function safeParseDate(value?: string | null) {
  if (!value) return null;
  const date = new Date(value);
  return Number.isFinite(date.getTime()) ? date : null;
}

function parseLogFileDate(fileName: string) {
  if (!fileName.startsWith(logFilePrefix) || !fileName.endsWith(logFileSuffix)) return null;
  const raw = fileName.slice(logFilePrefix.length, -logFileSuffix.length);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(raw)) return null;
  const date = new Date(`${raw}T00:00:00`);
  return Number.isFinite(date.getTime()) ? date : null;
}

function dateRangeForLogFile(fileName: string) {
  const start = parseLogFileDate(fileName);
  if (!start) return null;
  const end = new Date(start);
  end.setDate(end.getDate() + 1);
  return { start, end };
}

function filterLogFilesByDate(files: string[], input: LogQueryInput) {
  const dateFrom = safeParseDate(input.dateFrom);
  const dateTo = safeParseDate(input.dateTo);
  if (!dateFrom && !dateTo) return files.slice(0, maxFilteredScanFilesWithoutDate);

  return files.filter((fileName) => {
    const range = dateRangeForLogFile(fileName);
    if (!range) return false;
    if (dateFrom && range.end <= dateFrom) return false;
    if (dateTo && range.start > dateTo) return false;
    return true;
  });
}

function encodeCursor(row: FileSyncLog) {
  return Buffer.from(JSON.stringify({ createdAt: row.createdAt, id: row.id }), "utf8").toString("base64url");
}

function decodeCursor(cursor?: string) {
  if (!cursor) return null;

  try {
    const parsed = JSON.parse(Buffer.from(cursor, "base64url").toString("utf8")) as { createdAt?: unknown; id?: unknown };
    const createdAt = typeof parsed.createdAt === "string" ? parsed.createdAt : "";
    const id = typeof parsed.id === "string" ? parsed.id : "";
    if (!createdAt || !id || !safeParseDate(createdAt)) return null;
    return { createdAt, id };
  } catch {
    const legacyDate = safeParseDate(cursor);
    return legacyDate ? { createdAt: legacyDate.toISOString(), id: "" } : null;
  }
}

function compareLogRows(left: Pick<FileSyncLog, "createdAt" | "id">, right: Pick<FileSyncLog, "createdAt" | "id">) {
  const diff = new Date(right.createdAt).getTime() - new Date(left.createdAt).getTime();
  return diff || right.id.localeCompare(left.id);
}

function isAfterCursor(row: FileSyncLog, cursor: { createdAt: string; id: string } | null) {
  if (!cursor) return true;
  return compareLogRows(row, cursor) > 0;
}

function hasDeepFilters(input: LogQueryInput) {
  return Boolean(input.action || input.target || input.search || input.dateFrom || input.dateTo || input.levels?.length || input.statuses?.length);
}

function isHiddenByClearMarker(row: FileSyncLog, clearMarkers: Record<string, string>) {
  const cutoff = clearMarkers[String(row.connectionId)];
  if (!cutoff) return false;

  const createdAt = safeParseDate(row.createdAt);
  const cutoffDate = safeParseDate(cutoff);
  return Boolean(createdAt && cutoffDate && createdAt <= cutoffDate);
}

async function readRecentLogLines(filePath: string) {
  const handle = await open(/*turbopackIgnore: true*/ filePath, "r");
  try {
    const stat = await handle.stat();
    const length = Math.min(stat.size, recentLogTailBytes);
    if (length <= 0) return [];

    const buffer = Buffer.alloc(length);
    await handle.read(buffer, 0, length, stat.size - length);
    const text = buffer.toString("utf8");
    const lines = text.split(/\r?\n/).filter(Boolean);
    if (stat.size > length && lines.length > 0) lines.shift();
    return lines;
  } finally {
    await handle.close();
  }
}

async function countMatchingLogsInFile(filePath: string, input: LogQueryInput, clearMarkers: Record<string, string>) {
  let count = 0;
  let index = 0;
  const stream = createReadStream(/*turbopackIgnore: true*/ filePath, { encoding: "utf8" });
  const reader = createInterface({ input: stream, crlfDelay: Infinity });

  try {
    for await (const line of reader) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      try {
        const row = normalizeLogRow(JSON.parse(trimmed), `${path.basename(filePath)}:${index}`);
        if (row && !isHiddenByClearMarker(row, clearMarkers) && matchLog(row, input)) count += 1;
      } catch {
        // Ignore malformed legacy/manual lines.
      } finally {
        index += 1;
      }
    }
  } finally {
    reader.close();
  }

  return count;
}

async function scanLogFileNewestFirst(
  fileName: string,
  input: LogQueryInput,
  clearMarkers: Record<string, string>,
  cursor: { createdAt: string; id: string } | null,
  rows: FileSyncLog[],
  maxRows: number,
) {
  const filePath = path.join(/*turbopackIgnore: true*/ logsDir(), fileName);
  const lines = await readRecentLogLines(filePath);

  for (let index = lines.length - 1; index >= 0; index -= 1) {
    const line = lines[index];
    try {
      const row = normalizeLogRow(JSON.parse(line), `${fileName}:${index}`);
      if (!row || isHiddenByClearMarker(row, clearMarkers) || !isAfterCursor(row, cursor)) continue;
      if (matchLog(row, input)) rows.push(row);
      if (rows.length >= maxRows) return;
    } catch {
      // Ignore malformed legacy/manual lines.
    }
  }
}

function trimRows(rows: FileSyncLog[], maxRows: number) {
  if (rows.length <= maxRows) return;
  rows.sort(compareLogRows);
  rows.splice(maxRows);
}

async function scanLogFileStreaming(
  fileName: string,
  input: LogQueryInput,
  clearMarkers: Record<string, string>,
  cursor: { createdAt: string; id: string } | null,
  rows: FileSyncLog[],
  maxRows: number,
) {
  let total = 0;
  let index = 0;
  const stream = createReadStream(/*turbopackIgnore: true*/ path.join(/*turbopackIgnore: true*/ logsDir(), fileName), { encoding: "utf8" });
  const reader = createInterface({ input: stream, crlfDelay: Infinity });

  try {
    for await (const line of reader) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      try {
        const row = normalizeLogRow(JSON.parse(trimmed), `${fileName}:${index}`);
        if (!row || isHiddenByClearMarker(row, clearMarkers) || !isAfterCursor(row, cursor) || !matchLog(row, input)) continue;
        total += 1;
        rows.push(row);
        if (rows.length > maxRows * 3) trimRows(rows, maxRows);
      } catch {
        // Ignore malformed legacy/manual lines.
      } finally {
        index += 1;
      }
    }
  } finally {
    reader.close();
  }

  trimRows(rows, maxRows);
  return total;
}

async function listLogFiles() {
  try {
    const entries = await readdir(/*turbopackIgnore: true*/ logsDir(), { withFileTypes: true });
    return entries
      .filter((entry) => entry.isFile() && parseLogFileDate(entry.name))
      .map((entry) => entry.name)
      .sort((left, right) => right.localeCompare(left));
  } catch (error) {
    const code = (error as { code?: string }).code;
    if (code === "ENOENT") return [];
    throw error;
  }
}

function normalizeLogRow(raw: unknown, id: string): FileSyncLog | null {
  if (!raw || typeof raw !== "object") return null;
  const row = raw as Partial<FileSyncLog>;
  const connectionId = Number(row.connectionId);
  const action = typeof row.action === "string" ? row.action : "";
  const createdAt = typeof row.createdAt === "string" ? row.createdAt : "";
  if (!Number.isInteger(connectionId) || connectionId <= 0 || !action || !safeParseDate(createdAt)) return null;

  return {
    id: typeof row.id === "string" && row.id ? row.id : id,
    connectionId,
    action,
    actionLabel: typeof row.actionLabel === "string" && row.actionLabel ? row.actionLabel : logActionLabel(action),
    target: typeof row.target === "string" && row.target ? row.target : null,
    level: normalizeLogLevel(row.level),
    detail: typeof row.detail === "string" && row.detail ? row.detail : null,
    status: normalizeLogStatus(row.status),
    error: typeof row.error === "string" && row.error ? row.error : null,
    createdAt,
  };
}

function includesText(value: string | null | undefined, query: string) {
  return (value ?? "").toLowerCase().includes(query);
}

function includesAction(action: string, query: string) {
  return includesText(action, query) || includesText(logActionLabel(action), query);
}

function matchLog(row: FileSyncLog, input: LogQueryInput) {
  if (input.connectionId && row.connectionId !== input.connectionId) return false;
  if (input.levels?.length && !input.levels.includes(row.level)) return false;
  if (input.statuses?.length && !input.statuses.includes(row.status)) return false;
  if (input.action && !includesAction(row.action, input.action.toLowerCase())) return false;
  if (input.target && !includesText(row.target, input.target.toLowerCase())) return false;

  const createdAt = safeParseDate(row.createdAt);
  if (!createdAt) return false;
  const dateFrom = safeParseDate(input.dateFrom);
  const dateTo = safeParseDate(input.dateTo);
  if (dateFrom && createdAt < dateFrom) return false;
  if (dateTo && createdAt > dateTo) return false;

  if (input.search) {
    const query = input.search.toLowerCase();
    return includesAction(row.action, query)
      || includesText(row.actionLabel, query)
      || includesText(row.target, query)
      || includesText(row.detail, query)
      || includesText(row.error, query);
  }

  return true;
}

export async function cleanupOldLogs(_db?: unknown, retentionDays?: number) {
  const days = await getRetentionDaysForCleanup(retentionDays);
  const cutoff = Date.now() - days * 24 * 60 * 60 * 1000;
  const files = await listLogFiles();
  let deletedFiles = 0;

  await Promise.all(files.map(async (fileName) => {
    const date = parseLogFileDate(fileName);
    if (!date || date.getTime() >= cutoff) return;
    await rm(/*turbopackIgnore: true*/ path.join(/*turbopackIgnore: true*/ logsDir(), fileName), { force: true });
    deletedFiles += 1;
  }));

  return { count: deletedFiles, deletedFiles };
}

export async function clearLogs(input?: { connectionId?: number }) {
  const files = await listLogFiles();

  if (!input?.connectionId) {
    return withClearMarkersLock(async () => {
      let deletedFiles = 0;
      await Promise.all(files.map(async (fileName) => {
        await rm(/*turbopackIgnore: true*/ path.join(/*turbopackIgnore: true*/ logsDir(), fileName), { force: true });
        deletedFiles += 1;
      }));
      await rm(/*turbopackIgnore: true*/ clearMarkersPath(), { force: true });
      return { count: deletedFiles, deletedFiles };
    });
  }

  return withClearMarkersLock(async () => {
    const cutoff = new Date().toISOString();
    const clearMarkers = await readClearMarkers();
    const countFiles = files.slice(0, maxFilteredScanFilesWithoutDate);
    const count = await Promise.all(countFiles.map((fileName) => (
      countMatchingLogsInFile(path.join(/*turbopackIgnore: true*/ logsDir(), fileName), { ...input, limit: 1, dateTo: cutoff }, clearMarkers)
    ))).then((counts) => counts.reduce((total, value) => total + value, 0));
    await writeClearMarkers({ ...clearMarkers, [String(input.connectionId)]: cutoff });
    return { count };
  });
}

export async function listSyncLogs(input: LogQueryInput) {
  const limit = Math.min(Math.max(input.limit, 1), 500);
  const cursor = decodeCursor(input.cursor);
  const files = await listLogFiles();
  const clearMarkers = await readClearMarkers();
  const rows: FileSyncLog[] = [];
  const fullScan = hasDeepFilters(input);
  const scanFiles = fullScan ? filterLogFilesByDate(files, input) : files.slice(0, maxRecentScanFiles);
  const maxRows = limit + 1;
  let total = 0;

  for (const fileName of scanFiles) {
    if (rows.length >= maxRows && !fullScan) break;
    if (fullScan) {
      total += await scanLogFileStreaming(fileName, input, clearMarkers, cursor, rows, maxRows);
    } else {
      await scanLogFileNewestFirst(fileName, input, clearMarkers, cursor, rows, maxRows);
    }
  }

  rows.sort(compareLogRows);

  if (!fullScan) total = rows.length;
  const items = rows.slice(0, limit);
  const nextCursor = total > limit ? (items[items.length - 1] ? encodeCursor(items[items.length - 1]) : null) : null;
  return { logs: items, total, nextCursor };
}

export async function writeSyncLog(
  _db: unknown,
  input: {
    connectionId: number;
    action: string;
    target: string;
    detail?: unknown;
    status?: LogStatus;
    error?: string;
    level?: LogLevel;
  },
) {
  const status = input.status ?? "success";
  const level = levelFromStatus(status, input.level);
  const settings = await getLogSettingsForWrite();
  if (!shouldRecordLog(settings, level)) return null;

  const createdAt = new Date();
  const row: FileSyncLog = {
    id: `${createdAt.toISOString()}-${process.pid}-${Math.random().toString(36).slice(2, 10)}`,
    connectionId: input.connectionId,
    action: input.action,
    actionLabel: logActionLabel(input.action),
    target: input.target || null,
    level,
    detail: serializeDetail(input.detail),
    status,
    error: input.error || null,
    createdAt: createdAt.toISOString(),
  };

  await mkdir(/*turbopackIgnore: true*/ logsDir(), { recursive: true });
  await appendFile(/*turbopackIgnore: true*/ logFilePath(createdAt), `${JSON.stringify(row)}\n`, "utf8");
  return row;
}
