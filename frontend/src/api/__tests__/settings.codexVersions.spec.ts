import { beforeEach, describe, expect, it, vi } from "vitest";

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("../client", () => ({ apiClient: { get, post } }));

import { getOpenAICodexVersions, settingsAPI, syncOpenAICodexVersion } from "../admin/settings";

describe("admin Codex version API", () => {
  beforeEach(() => {
    get.mockReset();
    post.mockReset();
  });

  it("passes the official history cursor through the admin settings endpoint", async () => {
    const response = { versions: [], latest_version: "0.150.1", has_more: true, next_page: 7 };
    get.mockResolvedValue({ data: response });
    expect(await getOpenAICodexVersions(4)).toEqual(response);
    expect(get).toHaveBeenCalledWith("/admin/settings/openai-codex/versions", { params: { page: 4 } });
    expect(settingsAPI.getOpenAICodexVersions).toBe(getOpenAICodexVersions);
  });

  it("syncs independently without posting form overrides or changing the version mode", async () => {
    const response = {
      latest_version: "0.151.0", synced_version: "0.151.0", updated: true,
      defaults: { originator: "Codex Desktop", user_agent: "Codex Desktop/0.151.0", client_version: "0.151.0" },
    };
    post.mockResolvedValue({ data: response });
    expect(await syncOpenAICodexVersion()).toEqual(response);
    expect(post).toHaveBeenCalledWith("/admin/settings/openai-codex/sync");
    expect(settingsAPI.syncOpenAICodexVersion).toBe(syncOpenAICodexVersion);
  });

  it("propagates official source errors to the UI", async () => {
    const error = new Error("official source unavailable");
    post.mockRejectedValue(error);
    await expect(syncOpenAICodexVersion()).rejects.toBe(error);
  });
});
