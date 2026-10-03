import type { PrismaClient } from "@prisma/client";
import type { Sub2ApiAdminClient } from "@/server/clients/sub2api-admin";
import { formatRateMultiplier, normalizeRateMultiplier, ratesEqual } from "@/shared/rates";
import { writeSyncLog } from "@/server/sync-logs";

type AnnouncementRuleDb = Pick<PrismaClient, "announcementRule" | "setting">;

export function announcementRulePublishedAtKey(ruleId: number) {
  return `announcement_rule_last_published_at:${ruleId}`;
}

export async function recordAnnouncementRulePublishedAt(
  db: Pick<PrismaClient, "setting">,
  ruleId: number,
  publishedAt = new Date(),
) {
  const value = publishedAt.toISOString();
  await db.setting.upsert({
    where: { key: announcementRulePublishedAtKey(ruleId) },
    create: { key: announcementRulePublishedAtKey(ruleId), value },
    update: { value },
  });
  return publishedAt;
}

export type RateChangeAnnouncementContext = {
  action: string;
  connectionId: number;
  connectionName?: string | null;
  groupId: number;
  groupName: string;
  oldRate?: number | null;
  newRate: number;
  sourceSiteId?: number | null;
  sourceSiteName?: string | null;
  sourceGroupId?: string | null;
  sourceGroupName?: string | null;
  sourceRate?: number | null;
  sourceLabel?: string | null;
  ruleMode?: string | null;
  ruleExpression?: string | null;
  changedAt?: Date;
};

export const defaultRateAnnouncementTitleTemplate = "分组 {{groupName}} 倍率已调整为 {{newRate}}";
export const defaultRateAnnouncementContentTemplate = [
  "分组：{{groupName}} (#{{groupId}})",
  "原倍率：{{oldRate}}",
  "新倍率：{{newRate}}",
  "来源：{{sourceLabel}}",
  "时间：{{changedAt}}",
].join("\n");

function formatValue(value: unknown) {
  if (value === null || value === undefined || value === "") return "-";
  if (typeof value === "number") {
    return formatRateMultiplier(value);
  }
  if (value instanceof Date) return value.toLocaleString("zh-CN", { hour12: false });
  return String(value);
}

function templateValues(context: RateChangeAnnouncementContext) {
  const changedAt = context.changedAt ?? new Date();
  const sourceLabel = context.sourceLabel || (context.sourceGroupName || context.sourceGroupId
    ? `${context.sourceSiteName || "BL"} / ${context.sourceGroupName || context.sourceGroupId}`
    : context.action);

  return {
    action: context.action,
    connectionId: context.connectionId,
    connectionName: context.connectionName,
    groupId: context.groupId,
    groupName: context.groupName,
    oldRate: typeof context.oldRate === "number" ? normalizeRateMultiplier(context.oldRate) : context.oldRate,
    newRate: normalizeRateMultiplier(context.newRate),
    sourceSiteId: context.sourceSiteId,
    sourceSiteName: context.sourceSiteName,
    sourceGroupId: context.sourceGroupId,
    sourceGroupName: context.sourceGroupName,
    sourceRate: typeof context.sourceRate === "number" ? normalizeRateMultiplier(context.sourceRate) : context.sourceRate,
    sourceLabel,
    ruleMode: context.ruleMode,
    ruleExpression: context.ruleExpression,
    changedAt,
  };
}

export function renderAnnouncementTemplate(template: string, context: RateChangeAnnouncementContext) {
  const values = templateValues(context);
  return template.replace(/\{\{\s*([a-zA-Z0-9_]+)\s*\}\}/g, (_match, key: string) => formatValue(values[key as keyof typeof values]));
}

async function logAnnouncementRule(db: AnnouncementRuleDb, connectionId: number, detail: Record<string, unknown>, status: "success" | "failed", error?: string) {
  try {
    await writeSyncLog(db, {
      connectionId,
      action: "auto_announcement_rule",
      target: detail.ruleId ? `rule:${detail.ruleId}` : "rule",
      detail,
      status,
      error,
    });
  } catch {
    // Announcement logging should never affect the primary rate update path.
  }
}

function ruleMatchesTargetGroup(rule: { targetGroupIds?: number[] | null }, groupId: number) {
  const targetGroupIds = rule.targetGroupIds ?? [];
  return targetGroupIds.length === 0 || targetGroupIds.includes(groupId);
}

export async function publishRateChangeAnnouncements(input: {
  db: AnnouncementRuleDb;
  client: Sub2ApiAdminClient;
  context: RateChangeAnnouncementContext;
}) {
  const { db, client, context } = input;
  if (
    typeof context.oldRate === "number" &&
    Number.isFinite(context.oldRate) &&
    Number.isFinite(context.newRate) &&
    ratesEqual(context.oldRate, context.newRate)
  ) {
    return { ok: true, published: 0, skipped: true };
  }

  let rules: Awaited<ReturnType<AnnouncementRuleDb["announcementRule"]["findMany"]>>;
  try {
    rules = await db.announcementRule.findMany({
      where: { connectionId: context.connectionId, enabled: true },
      orderBy: { id: "asc" },
    });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    await logAnnouncementRule(db, context.connectionId, {
      action: context.action,
      groupId: context.groupId,
      groupName: context.groupName,
      oldRate: context.oldRate,
      newRate: context.newRate,
    }, "failed", message);
    return { ok: false, published: 0, skipped: false, error: message };
  }

  let published = 0;
  for (const rule of rules) {
    const detail = {
      ruleId: rule.id,
      ruleName: rule.name,
      groupId: context.groupId,
      groupName: context.groupName,
      oldRate: context.oldRate,
      newRate: context.newRate,
      targetGroupIds: rule.targetGroupIds,
    };

    if (!ruleMatchesTargetGroup(rule, context.groupId)) continue;

    const title = renderAnnouncementTemplate(rule.titleTemplate, context).trim();
    const content = renderAnnouncementTemplate(rule.contentTemplate, context).trim();
    const publishDetail = { ...detail, title };

    if (!title || !content) {
      await logAnnouncementRule(db, context.connectionId, publishDetail, "failed", "Rendered title or content is empty");
      continue;
    }

    try {
      await client.createAnnouncement({
        title,
        content,
        status: rule.status,
        notify_mode: rule.notifyMode,
      });
      published += 1;
      try {
        const publishedAt = await recordAnnouncementRulePublishedAt(db, rule.id);
        await logAnnouncementRule(db, context.connectionId, { ...publishDetail, publishedAt }, "success");
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        await logAnnouncementRule(
          db,
          context.connectionId,
          publishDetail,
          "failed",
          `Announcement published but timestamp persistence failed: ${message}`,
        );
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      await logAnnouncementRule(db, context.connectionId, publishDetail, "failed", message);
    }
  }

  return { ok: true, published, skipped: false };
}
