import { confirmAction, confirmChoice } from "@/components/ConfirmActionDialog";
import { i18next } from "@/i18n";
import { Trash } from "@/icons/leafen";
import { RefreshInCircleIcon } from "@/icons/leafen/notionMarkIcons";
import { activeDateLocale } from "@/lib/i18n";
import token from "./McpToken.module.css";

/** A date with its time and zone, as Notion's names the day an old token stops. */
export const dateAndTime = (value: number) => new Intl.DateTimeFormat(activeDateLocale(), {
  year: "numeric", month: "long", day: "numeric", hour: "numeric", minute: "2-digit", timeZoneName: "short",
}).format(value);

const REVOKE_LATER_MS = 7 * 24 * 3600 * 1000;

/**
 * The developer tools' question before a token is made again, as Notion's
 * (seen 2026-11-21, an API token's "토큰 새로고침"; its MCP tokens' the same
 * card, 2026-11-09): its grey mark, a clockwise arrow in a ring; when the
 * token it replaces stops — in seven days, the first chosen, with the day it
 * stops, or at once — over the filled red action; the "×" focused as it
 * opens (seen in two tabs). Resolves "later", "now", or null when cancelled.
 */
export async function askTokenRefresh(kind: "mcpToken" | "internal"): Promise<"later" | "now" | null> {
  const words = (key: string, values?: Record<string, string>) => i18next.t(`workspaceSettingsDialog:developerTools.${kind}.${key}`, values);
  const answer = await confirmChoice({
    large: true, icon: <RefreshInCircleIcon size="card" aria-hidden="true" />, message: words("refreshConfirm"),
    detail: words("refreshDetail"), choicesLabel: words("refreshWhen"),
    choices: [
      { value: "later", label: words("revokeLater"), description: words("revokeLaterNote", { date: dateAndTime(Date.now() + REVOKE_LATER_MS) }) },
      { value: "now", label: words("revokeNow"), description: words("revokeNowNote") },
    ],
    confirmLabel: words("refresh"), danger: "filled", closeFirst: true,
  });
  return answer === "later" || answer === "now" ? answer : null;
}

/**
 * The developer tools' question before a connection or an MCP token goes, as
 * Notion's "위험 구역 › 삭제하기" (seen 2026-11-21 on an internal connection):
 * a red bin, the question, its line and the filled red action. A connection's
 * words are its own; an MCP token passes its.
 */
export function askDeveloperDelete(words?: { message: string; detail: string; action: string }) {
  const t = (key: string) => i18next.t(`workspaceSettingsDialog:developerTools.connection.${key}`);
  return confirmAction({
    large: true, icon: <span className={token.dangerMark}><Trash size="card" aria-hidden="true" /></span>,
    message: words?.message ?? t("removeConfirm"), detail: words?.detail ?? t("removeDetail"),
    confirmLabel: words?.action ?? t("removeAction"), danger: "filled",
  });
}
