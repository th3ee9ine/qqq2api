import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

const currentDir = dirname(fileURLToPath(import.meta.url));
const chartsDir = resolve(currentDir, "..");

const readChart = (name: string) =>
  readFileSync(resolve(chartsDir, name), "utf8");

describe("system-wide distribution charts", () => {
  it.each([
    "GroupDistributionChart.vue",
    "EndpointDistributionChart.vue",
    "ModelDistributionChart.vue",
  ])("does not expose per-user drilldown in %s", (name) => {
    const source = readChart(name);
    expect(source).not.toContain("UserBreakdown");
    expect(source).not.toContain("getUserBreakdown");
    expect(source).not.toContain("enableBreakdown");
    expect(source).not.toContain("startDate?:");
    expect(source).not.toContain("filters?:");
  });

  it("does not expose a user spending ranking mode", () => {
    const source = readChart("ModelDistributionChart.vue");
    expect(source).not.toContain("UserSpendingRanking");
    expect(source).not.toContain("enableRankingView");
    expect(source).not.toContain("spendingRanking");
    expect(source).not.toContain("ranking-click");
  });

  it("does not retain dashboard API clients or types for per-user statistics", () => {
    const apiSource = readFileSync(
      resolve(currentDir, "../../../api/admin/dashboard.ts"),
      "utf8",
    );
    const typesSource = readFileSync(
      resolve(currentDir, "../../../types/index.ts"),
      "utf8",
    );

    expect(apiSource).not.toMatch(/users-(trend|ranking|usage)/);
    expect(apiSource).not.toContain("user-breakdown");
    expect(typesSource).not.toContain("UserBreakdownItem");
    expect(typesSource).not.toContain("UserSpendingRankingItem");
    expect(typesSource).not.toContain("UserUsageTrendPoint");
  });
});
