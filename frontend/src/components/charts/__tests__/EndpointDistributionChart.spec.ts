import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

import EndpointDistributionChart from "../EndpointDistributionChart.vue";

vi.mock("vue-i18n", async () => {
  const actual = await vi.importActual<typeof import("vue-i18n")>("vue-i18n");
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  };
});

vi.mock("vue-chartjs", () => ({
  Doughnut: {
    props: ["data"],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}));

describe("EndpointDistributionChart", () => {
  it("renders system endpoint totals without interactive user drilldown", () => {
    const wrapper = mount(EndpointDistributionChart, {
      props: {
        endpointStats: [
          {
            endpoint: "/v1/messages",
            requests: 3,
            total_tokens: 120,
            cost: 0.6,
            actual_cost: 0.4,
          },
        ],
      },
      global: { stubs: { LoadingSpinner: true } },
    });

    expect(wrapper.findAll("tbody tr")).toHaveLength(1);
    expect(wrapper.find("tbody tr").text()).toContain("/v1/messages");
    expect(wrapper.find("tbody tr").classes()).not.toContain("cursor-pointer");
  });
});
