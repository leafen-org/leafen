import { i18next } from "@/i18n";
import type { TextSpan } from "@/lib/types";
import { escapeHtml } from "./richtextHtmlEscaping";

/**
 * A teamspace's or a group's mention, which the server keeps by its address
 * as a database mention nothing answers to (notion_markdown_kept_data_sources.go),
 * is shown as Notion shows one of nothing (seen 2026-11-22): a teamspace's
 * as a grey bar for a name, as a person's of no one is but without its "@";
 * a group's as "@익명의 그룹", all in the mention's grey.
 */
export type KeptAddressMention = "teamspace" | "group";

export function keptAddressMention(span: TextSpan): KeptAddressMention | undefined {
  if (span.mention !== "database") return undefined;
  const scheme = /^(teamspace|group):\/\//u.exec(span.databaseId ?? "")?.[1];
  return scheme === "teamspace" || scheme === "group" ? scheme : undefined;
}

/** What such a mention shows; a teamspace's words stay under its bar, unseen. */
export function keptAddressMentionHtml(kind: KeptAddressMention, words: string) {
  return kind === "group"
    ? `@${escapeHtml(String(i18next.t("blockItem:pageLink.mentionAnonymousGroup")))}`
    : `<span data-person-missing="">${escapeHtml(words)}</span>`;
}
