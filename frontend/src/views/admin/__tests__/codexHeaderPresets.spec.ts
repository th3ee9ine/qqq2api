import { describe, expect, it } from "vitest";
import { buildCodexUserAgentTemplate, compareCodexVersions, isStableCodexVersion, setCodexTemplateVersion } from "../codexHeaderPresets";

describe("Codex header templates", () => {
  it("orders numeric stable releases rather than strings", () => {
    expect(compareCodexVersions("0.100.0", "0.99.1")).toBeGreaterThan(0);
    expect(isStableCodexVersion("0.150.1")).toBe(true);
    expect(isStableCodexVersion("0.150.1-alpha.1")).toBe(false);
  });
  it("preserves independent app builds and only rewrites matching engine trailers", () => {
    expect(setCodexTemplateVersion("Codex Desktop/0.150.1 (Mac OS; arm64) unknown (Codex Desktop; 26.820.60940)", "0.140.0")).toBe("Codex Desktop/0.140.0 (Mac OS; arm64) unknown (Codex Desktop; 26.820.60940)");
    expect(setCodexTemplateVersion("codex-tui/0.150.1 (Linux; x86_64) unknown (codex-tui; 0.150.1)", "0.140.0")).toBe("codex-tui/0.140.0 (Linux; x86_64) unknown (codex-tui; 0.140.0)");
    expect(setCodexTemplateVersion("codex_vscode/0.150.1 (Linux; x86_64) vscode (codex_vscode; 0.9.8)", "0.140.0")).toContain("(codex_vscode; 0.9.8)");
  });
  it("builds paired non-desktop templates without borrowing the Desktop app trailer", () => {
    expect(buildCodexUserAgentTemplate("codex-tui", "0.140.0", "Codex Desktop/0.150.1 (Mac OS 26.2.0; arm64) unknown (Codex Desktop; 26.820.60940)")).toBe("codex-tui/0.140.0 (Mac OS 26.2.0; arm64) unknown");
    expect(buildCodexUserAgentTemplate("codex_cli_rs", "0.140.0", "", true)).toBe("codex_cli_rs/0.140.0 (Linux 6.8.0; x86_64) unknown");
  });
});
