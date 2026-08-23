import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const currentDir = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(resolve(currentDir, "../GroupsView.vue"), "utf8");
const enOverview = readFileSync(
  resolve(currentDir, "../../../i18n/locales/en/admin/overview.ts"),
  "utf8",
);
const zhOverview = readFileSync(
  resolve(currentDir, "../../../i18n/locales/zh/admin/overview.ts"),
  "utf8",
);

describe("GroupsView retired subscription billing", () => {
  it("does not expose subscription limits or peak-rate controls", () => {
    expect(source).not.toContain("#cell-billing_type");
    expect(source).not.toContain("admin.groups.subscription");
    expect(source).not.toContain("admin.groups.peakRate");
    expect(source).not.toContain("createForm.subscription_type");
    expect(source).not.toContain("editForm.subscription_type");
    expect(source).not.toContain("createForm.daily_limit_usd");
    expect(source).not.toContain("editForm.daily_limit_usd");
    expect(source).not.toContain("createForm.peak_rate_enabled");
    expect(source).not.toContain("editForm.peak_rate_enabled");
  });

  it("normalizes both create and update payloads to standard billing", () => {
    expect(source.match(/subscription_type: "standard" as const/g)).toHaveLength(2);
    expect(source.match(/daily_limit_usd: null/g)).toHaveLength(2);
    expect(source.match(/weekly_limit_usd: null/g)).toHaveLength(2);
    expect(source.match(/monthly_limit_usd: null/g)).toHaveLength(2);
    expect(source.match(/peak_rate_enabled: false/g)).toHaveLength(2);
  });

  it("removes retired group billing translations", () => {
    expect(enOverview).not.toContain("deleteConfirmSubscription");
    expect(enOverview).not.toContain("Enable peak rate multiplier");
    expect(enOverview).not.toContain("title: 'Subscription Settings'");
    expect(zhOverview).not.toContain("启用高峰倍率");
    expect(zhOverview).not.toContain("title: '订阅设置'");
  });
});
