import { describe, expect, it } from "vitest";
import {
  buildCodexUserAgentTemplate,
  compareCodexVersions,
  defaultCodexClientVersion,
  isStableCodexVersion,
  setCodexTemplateVersion,
} from "../codexHeaderPresets";

describe("Codex header templates", () => {
  it("orders numeric stable releases rather than strings", () => {
    expect(compareCodexVersions("0.100.0", "0.99.1")).toBeGreaterThan(0);
    expect(defaultCodexClientVersion).toBe("0.154.0");
    expect(isStableCodexVersion("0.154.0")).toBe(true);
    expect(isStableCodexVersion("0.154.0-alpha.1")).toBe(false);
  });
  it("preserves independent app builds and only rewrites matching engine trailers", () => {
    expect(setCodexTemplateVersion(
      "Codex Desktop/0.154.0 (Mac OS 26.2.0; arm64) Apple_Terminal/466 (Codex Desktop; 26.911.61220)",
      "0.200.1",
    )).toBe("Codex Desktop/0.200.1 (Mac OS 26.2.0; arm64) Apple_Terminal/466 (Codex Desktop; 26.911.61220)");
    expect(setCodexTemplateVersion(
      "codex-tui/0.154.0 (Mac OS 26.2.0; arm64) Apple_Terminal/466 (codex-tui; 0.154.0)",
      "0.200.1",
    )).toBe("codex-tui/0.200.1 (Mac OS 26.2.0; arm64) Apple_Terminal/466 (codex-tui; 0.200.1)");
    expect(setCodexTemplateVersion(
      "codex_cli_rs/0.154.0 (Ubuntu 22.4.0; x86_64) xterm-256color",
      "0.200.1",
    )).toBe("codex_cli_rs/0.200.1 (Ubuntu 22.4.0; x86_64) xterm-256color");
    expect(setCodexTemplateVersion(
      "codex_vscode/0.154.0 (Linux; x86_64) vscode (codex_vscode; 0.9.8)",
      "0.200.1",
    )).toBe("codex_vscode/0.200.1 (Linux; x86_64) vscode (codex_vscode; 0.9.8)");
  });
  it("builds the three verified native templates exactly", () => {
    expect(buildCodexUserAgentTemplate("codex-tui", "0.154.0")).toBe(
      "codex-tui/0.154.0 (Mac OS 26.2.0; arm64) Apple_Terminal/466 (codex-tui; 0.154.0)",
    );
    expect(buildCodexUserAgentTemplate("codex_cli_rs", "0.154.0")).toBe(
      "codex_cli_rs/0.154.0 (Ubuntu 22.4.0; x86_64) xterm-256color",
    );
    expect(buildCodexUserAgentTemplate("Codex Desktop", "0.154.0")).toBe(
      "Codex Desktop/0.154.0 (Mac OS 26.2.0; arm64) Apple_Terminal/466 (Codex Desktop; 26.911.61220)",
    );
    expect(buildCodexUserAgentTemplate("codex_vscode", "0.154.0")).toBe("");
    expect(buildCodexUserAgentTemplate("codex-tui", "latest")).toBe("");
  });
});
