import { i18next } from "@/i18n";

/**
 * What a database template's mention becomes when the template is used, as
 * Notion says it after the mention in its "@" menu and in the mention's tip
 * (seen 2026-11-09): "지금" the time and "오늘" the date of the duplicate;
 * "나" says nothing.
 */
export function templateMentionDescription(template: "today" | "now" | "me") {
  return template === "now" ? i18next.t("blockItem:mention.template.nowDescription")
    : template === "today" ? i18next.t("blockItem:mention.template.todayDescription") : "";
}
