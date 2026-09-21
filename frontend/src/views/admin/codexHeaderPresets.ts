export const defaultCodexClientVersion = "0.154.0";

// Verified native identity templates. Other client identities remain available
// through the manual Originator and User-Agent inputs.
export const codexOriginators = [
  "Codex Desktop",
  "codex-tui",
  "codex_cli_rs",
] as const;

export function compareCodexVersions(a: string, b: string): number {
  const left = a.split(".").map(Number);
  const right = b.split(".").map(Number);
  for (let i = 0; i < 3; i++) {
    if (left[i] !== right[i]) return (left[i] ?? 0) - (right[i] ?? 0);
  }
  return 0;
}

export function isStableCodexVersion(value: string): boolean {
  return /^\d+\.\d+\.\d+$/.test(value);
}

export function setCodexTemplateVersion(userAgent: string, version: string): string {
  if (!isStableCodexVersion(version)) return userAgent;
  const match = userAgent.match(/^([^/]+)\/([^\s(]+)/);
  if (!match) return userAgent;
  let result = userAgent.replace(/^[^/]+\/[^\s(]+/, `${match[1]}/${version}`);
  // Only CLI/TUI/exec trailers share the engine version domain. Desktop and
  // editor app builds are independent and must never follow rust-v releases.
  if (["codex_cli_rs", "codex-tui", "codex_exec"].includes(match[1]!)) {
    const previousTrailer = `(${match[1]}; ${match[2]})`;
    if (result.endsWith(previousTrailer)) {
      result = result.slice(0, -previousTrailer.length) + `(${match[1]}; ${version})`;
    }
  }
  return result;
}

export function buildCodexUserAgentTemplate(
  originator: string,
  version: string,
): string {
  if (!isStableCodexVersion(version)) return "";
  switch (originator) {
    case "Codex Desktop":
      return `Codex Desktop/${version} (Mac OS 26.2.0; arm64) Apple_Terminal/466 (Codex Desktop; 26.911.61220)`;
    case "codex-tui":
      return `codex-tui/${version} (Mac OS 26.2.0; arm64) Apple_Terminal/466 (codex-tui; ${version})`;
    case "codex_cli_rs":
      return `codex_cli_rs/${version} (Ubuntu 22.4.0; x86_64) xterm-256color`;
    default:
      return "";
  }
}
