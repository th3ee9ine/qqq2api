import { beforeEach, describe, expect, it, vi } from "vitest";

const { get, put } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }));
vi.mock("../client", () => ({ apiClient: { get, put } }));

import {
  appendAuthSourceDefaultsToUpdateRequest,
  buildAuthSourceDefaultsState,
  getSettings,
  normalizeAccountSchedulingThresholdsMap,
  normalizePlatformQuotasMap,
  sanitizeAccountSchedulingThresholdsMap,
  sanitizePlatformQuotasMap,
  updateSettings,
  type UpdateSettingsRequest,
} from "../admin/settings";

describe("admin Grok settings", () => {
  beforeEach(() => {
    get.mockReset();
    put.mockReset();
  });

  it("preserves a custom model, an explicit disabled map and a regional upstream on load and save", async () => {
    const settings = {
      grok_default_text_model: "grok-custom-deployment",
      grok_cross_client_model_map_enabled: false,
      grok_default_base_url_mode: "eu-west-1",
    };
    get.mockResolvedValue({ data: settings });
    put.mockResolvedValue({ data: settings });

    const loaded = await getSettings();
    expect(loaded).toEqual(settings);
    expect(await updateSettings(loaded)).toEqual(settings);
    expect(put).toHaveBeenCalledWith("/admin/settings", settings);
  });

  it("keeps Grok quota zeroes and custom caps through default normalization and saving", () => {
    const grok = { daily: 0, weekly: 12.5, monthly: 40 };
    const quotas = sanitizePlatformQuotasMap(normalizePlatformQuotasMap({ grok }));
    expect(quotas.grok).toEqual(grok);
    expect(Object.keys(quotas)).toEqual(["anthropic", "openai", "grok"]);

    const state = buildAuthSourceDefaultsState({ auth_source_default_email_platform_quotas: { grok } });
    const request: UpdateSettingsRequest = {};
    appendAuthSourceDefaultsToUpdateRequest(request, state);
    expect(request.auth_source_default_email_platform_quotas?.grok).toEqual(grok);
  });

  it("preserves the Grok scheduling threshold without re-enabling retired providers", () => {
    const normalized = normalizeAccountSchedulingThresholdsMap({ grok: 82.9 });
    expect(normalized).toEqual({ openai: 100, anthropic: 100, grok: 82 });
    expect(sanitizeAccountSchedulingThresholdsMap(normalized).grok).toBe(82);
    expect(normalizeAccountSchedulingThresholdsMap().grok).toBe(100);
  });
});
