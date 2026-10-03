import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {
  announcementRulePublishedAtKey,
  publishRateChangeAnnouncements,
  recordAnnouncementRulePublishedAt,
} from "./announcement-rules";

test("successful announcement publication records its latest timestamp in settings", async () => {
  let upsert: unknown;
  const publishedAt = new Date("2026-07-29T00:12:34.000Z");
  const db = {
    setting: {
      upsert: async (input: unknown) => {
        upsert = input;
        return {};
      },
    },
  };

  await recordAnnouncementRulePublishedAt(db as never, 27, publishedAt);

  assert.equal(announcementRulePublishedAtKey(27), "announcement_rule_last_published_at:27");
  assert.deepEqual(upsert, {
    where: { key: "announcement_rule_last_published_at:27" },
    create: {
      key: "announcement_rule_last_published_at:27",
      value: "2026-07-29T00:12:34.000Z",
    },
    update: { value: "2026-07-29T00:12:34.000Z" },
  });
});

test("successful remote publication persists the rule timestamp", async () => {
  const logDir = await mkdtemp(path.join(os.tmpdir(), "ops-announcement-test-"));
  const previousLogDir = process.env.S2A_LOG_DIR;
  process.env.S2A_LOG_DIR = logDir;
  await writeFile(path.join(logDir, "settings.json"), JSON.stringify({
    enabled: false,
    retentionDays: 30,
    minLevel: "info",
  }));

  let created: unknown;
  let marker: { create?: { key?: string; value?: string } } | undefined;
  try {
    const result = await publishRateChangeAnnouncements({
      db: {
        announcementRule: {
          findMany: async () => [{
            id: 8,
            name: "rate update",
            enabled: true,
            titleTemplate: "{{groupName}} updated",
            contentTemplate: "{{oldRate}} -> {{newRate}}",
            targetGroupIds: [],
            status: "active",
            notifyMode: "silent",
          }],
        },
        setting: {
          upsert: async (input: { create?: { key?: string; value?: string } }) => {
            marker = input;
            return {};
          },
        },
      } as never,
      client: {
        createAnnouncement: async (input: unknown) => {
          created = input;
          return {};
        },
      } as never,
      context: {
        action: "apply_group_rate_rule",
        connectionId: 1,
        groupId: 17,
        groupName: "Production",
        oldRate: 0.5,
        newRate: 0.7,
      },
    });

    assert.equal(result.published, 1);
    assert.deepEqual(created, {
      title: "Production updated",
      content: "0.5 -> 0.7",
      status: "active",
      notify_mode: "silent",
    });
    assert.equal(marker?.create?.key, "announcement_rule_last_published_at:8");
    assert.equal(Number.isFinite(Date.parse(marker?.create?.value ?? "")), true);
  } finally {
    if (previousLogDir === undefined) delete process.env.S2A_LOG_DIR;
    else process.env.S2A_LOG_DIR = previousLogDir;
    await rm(logDir, { recursive: true, force: true });
  }
});
