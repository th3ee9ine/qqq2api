import { beforeEach, describe, expect, it, vi } from "vitest";

const { get, put } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }));
vi.mock("../client", () => ({ apiClient: { get, put } }));

import {
  getOpenAIOAuthTTFTGuardSettings,
  settingsAPI,
  updateOpenAIOAuthTTFTGuardSettings,
} from "../admin/settings";

describe("admin OpenAI OAuth TTFT guard API", () => {
  beforeEach(() => {
    get.mockReset();
    put.mockReset();
  });

  it("loads and saves the recovery interval through the dedicated endpoint", async () => {
    const settings = { enabled: true, recovery_interval_minutes: 30 };
    get.mockResolvedValue({ data: settings });
    put.mockResolvedValue({ data: settings });

    expect(await getOpenAIOAuthTTFTGuardSettings()).toEqual(settings);
    expect(get).toHaveBeenCalledWith("/admin/settings/openai-oauth-ttft-guard");
    expect(await updateOpenAIOAuthTTFTGuardSettings(settings)).toEqual(settings);
    expect(put).toHaveBeenCalledWith("/admin/settings/openai-oauth-ttft-guard", settings);
    expect(settingsAPI.getOpenAIOAuthTTFTGuardSettings).toBe(getOpenAIOAuthTTFTGuardSettings);
    expect(settingsAPI.updateOpenAIOAuthTTFTGuardSettings).toBe(updateOpenAIOAuthTTFTGuardSettings);
  });
});
