import { i18next } from "@/i18n";

/**
 * What a person gives an agent on Leafen's own computer: it connects by the
 * server's program, which asks it to put the question to its person in the
 * conversation first and connects only on their yes (`leafen connect`). No
 * browser and no Leafen page: only a chat app's connector, which signs in by
 * MCP's standard sign-in, still asks there. In the person's language with
 * the line that has the agent speak it, as the connection text is
 * (2026-10-16); the command and the program's places stay as they are
 * (2026-11-25: the box read in English on every screen).
 */
export function agentConnectThisComputerText(): string {
  const t = (key: string, options?: Record<string, unknown>) =>
    String(i18next.t(`agentPrompt:${key}`, { ...options, interpolation: { escapeValue: false } }));
  return [
    t("thisComputer", {
      command: `\`leafen connect --name "${t("nameHint")}"\``,
      mac: "/Applications/Leafen.app/Contents/MacOS/leafen-desktop",
      windows: "leafen-desktop.exe",
      docker: "`docker compose exec leafen leafen`",
      source: "dist/leafen",
    }),
    t("talk"),
  ].join("\n");
}
