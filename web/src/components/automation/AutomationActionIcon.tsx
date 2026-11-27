import type { ReactNode } from "react";
import { ArchiveIcon, Bell, Copy, DeveloperIcon, EnvelopeIcon, GridIcon, LinkIcon, OpenInNew, Plus, RenameInPlaceIcon, SlidersIcon, SmileIcon } from "@/icons/leafen";
import { QuestionCircleIcon, VariablesIcon } from "@/icons/leafen/automationIcons";
import { ServiceIcon } from "@/icons/leafen/serviceIcons";
import type { TFunction } from "i18next";
import type { AutomationActionType } from "./automationActionModel";
import styles from "./automationActionEditor.module.css";
import markStyles from "./automationCardMark.module.css";

type IconSize = "small" | "regular";

// Each action's own icon, as Notion's list of actions (seen 2026-11-03):
// sliders for editing a property, a pencil on a line for editing pages,
// braces for a webhook, Slack's mark in its frame and an equals sign in a
// square for variables, an archive box for archiving pages (2026-11-15), a face
// for a page's icon (2026-11-21);
// the mail is Leafen's own, not Gmail, so an
// envelope. Every action has one at the same size, so each name in the list
// starts at the same x, as Notion's (seen 2026-11-08: all at 639). Its card
// and an automation list's card show the same icon, smaller (Notion's, seen
// 2026-11-09 for a webhook's braces).
const ICONS: Record<AutomationActionType, (size: IconSize) => ReactNode> = {
  insert_blocks: (size) => <Copy size={size} />,
  show_confirmation: (size) => <QuestionCircleIcon size={size} />,
  open_page: (size) => <OpenInNew size={size} />,
  // A form opens as a page does, Notion's ↗ (seen 2026-11-05).
  open_form: (size) => <OpenInNew size={size} />,
  open_url: (size) => <LinkIcon size={size} />,
  run_app_action: (size) => <GridIcon size={size} />,
  edit_property: (size) => <SlidersIcon size={size} />,
  add_page: (size) => <Plus size={size} />,
  edit_pages: (size) => <RenameInPlaceIcon size={size} />,
  archive_pages: (size) => <ArchiveIcon size={size} />,
  set_page_icon: (size) => <SmileIcon size={size} />,
  send_notification: (size) => <Bell size={size} />,
  send_email: (size) => <EnvelopeIcon size={size} />,
  send_webhook: (size) => <DeveloperIcon size={size} />,
  send_slack: (size) => <ServiceIcon service="slack" name="Slack" size={size} />,
  define_variables: (size) => <VariablesIcon size={size} />,
};

// The mark's colour on its card, as Notion's (seen 2026-10-10, 2026-11-08):
// blocks and a new page in blue, the rest in gray — any of them red while its
// card asks for what it lacks.
const BLUE_MARKS = new Set<AutomationActionType>(["insert_blocks", "add_page"]);

// On an automation list's card a notification's mark is blue too, beside the
// page added's (Notion's, seen 2026-11-08).
const LIST_TONES: Partial<Record<AutomationActionType, "blue" | "gray">> = { send_notification: "blue" };

const markTone = (type: AutomationActionType) => BLUE_MARKS.has(type) ? "blue" : "gray";

/** An action's mark colour, which an automation list's card tints its mark with. */
export function automationActionTone(type: AutomationActionType) {
  return LIST_TONES[type] ?? markTone(type);
}

/** An action's icon in the list it is chosen from. */
export function AutomationActionListIcon({ type }: { type: AutomationActionType }) {
  return <span className={styles.listIcon} aria-hidden="true">{ICONS[type]("regular")}</span>;
}

// The lacks a red chip names too: where its pages go, which its tip asks for (destination).
const DESTINATION_PROBLEMS = new Set(["addPageTarget", "editPagesTarget", "archivePagesTarget"]);

/**
 * What a red card tips under its head, as Notion's (seen 2026-11-21 in a
 * database button's editor and a database automation's): a page icon's lack
 * in its own words, "아이콘 선택"; another the sentence its red chip has —
 * where its pages go to add, or any other to fix — in the same words in each
 * editor (a database's automation, a database's button, a page's button).
 */
export function automationMarkTip(t: TFunction, problem: string | null) {
  if (!problem) return undefined;
  const key = problem.slice(problem.lastIndexOf(".") + 1);
  if (key === "pageIcon") return t(problem);
  return t(`databaseView:actionChipProblems.${DESTINATION_PROBLEMS.has(key) ? "destination" : "general"}`);
}

/**
 * An action's mark on its card; red while the card asks for what it lacks
 * (`warn`), standing over the grip then, so pointing at it tips the card's
 * lack (AutomationActionCard's problemTip) — or its own `tip`, a sentence
 * card's.
 */
export function AutomationActionIcon({ type, warn = false, tip }: { type: AutomationActionType; warn?: boolean; tip?: string }) {
  return <span className={warn ? `${styles.actionIcon} ${markStyles.warned}` : styles.actionIcon} data-tone={warn ? "red" : markTone(type)}
    data-automation-action-icon={type} data-warning={warn || undefined} data-icon-tip={warn ? tip : undefined}
    aria-hidden="true">{ICONS[type]("small")}</span>;
}
