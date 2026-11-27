import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown } from "@/icons/leafen";
import { WarningFillIcon } from "@/icons/leafen/automationIcons";
import type { SendNotificationAutomationAction } from "@/lib/types";
import { AutomationCardMenu } from "./AutomationActionMenus";
import styles from "./AutomationEditProperty.module.css";

/** Whom a notification goes to, as Notion's chip names it: people picked, or a person property of the page. */
export type AutomationRecipientKind = NonNullable<SendNotificationAutomationAction["recipientKind"]>;

/**
 * The kind a notification's recipients are of: the one its chip was set to, kept with nobody chosen yet and as the
 * last one is taken away — Notion's "특정 사용자" stays, its "사람" red, when reopened (seen 2026-11-21) — else the
 * one its recipients are of; none while it has neither.
 */
export function automationRecipientKindOf(action: SendNotificationAutomationAction): AutomationRecipientKind | null {
  if (action.recipientKind) return action.recipientKind;
  const sources = action.recipientSources ?? [];
  if (!action.recipientIds.length && sources.length && sources.every((source) => source.kind === "person_property")) return "property";
  return action.recipientIds.length || sources.length ? "people" : null;
}

/** A notification's recipients kept to a kind, as choosing the other starts it over; the kind is kept with them. */
export function automationRecipientsOfKind(action: SendNotificationAutomationAction, kind: AutomationRecipientKind): SendNotificationAutomationAction {
  const sources = action.recipientSources ?? [];
  return kind === "property"
    ? { ...action, recipientKind: kind, recipientIds: [], recipientSources: sources.filter((source) => source.kind === "person_property") }
    : { ...action, recipientKind: kind, recipientSources: sources.filter((source) => source.kind !== "person_property") };
}

/**
 * A notification card's chip, as Notion's (seen 2026-10-24): "받는 사람
 * 선택 ⌄" in grey until a kind is chosen, then "특정 사용자" or "사용자
 * 속성" — picked from a short list under it, on a phone from the screen's bottom.
 */
export function AutomationRecipientKindChip({ kind, number, warn = false, onChoose }: {
  kind: AutomationRecipientKind | null; number: number;
  /** Asked for with none chosen: "⚠ 받는 사람 선택" in red, as Notion's (seen 2026-11-08). */
  warn?: boolean;
  onChoose(kind: AutomationRecipientKind): void;
}) {
  const { t } = useTranslation("databaseView");
  const trigger = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const label = kind === "people" ? t("recipientKindPeople") : kind === "property" ? t("recipientKindProperty") : t("recipientKindChoose");
  return <span className={styles.kindWrap}>
    <button ref={trigger} type="button" className={styles.kindChip} aria-label={`${t("recipientKind")} ${number}: ${label}`}
      aria-haspopup="menu" aria-expanded={open} aria-invalid={warn || undefined} data-automation-recipient-kind={kind ?? ""}
      onClick={() => setOpen((value) => !value)}>
      {warn ? <WarningFillIcon size="tiny" aria-hidden="true" /> : null}{label}<ChevronDown size="tiny" aria-hidden="true" />
    </button>
    {/* Notion's 180px list right under the chip, lined up with its end (seen 2026-11-08). */}
    {open ? <AutomationCardMenu trigger={trigger} width={180} align="end" onClose={() => setOpen(false)}
      choices={(["people", "property"] as const).map((key) => ({ key, label: t(key === "people" ? "recipientKindPeople" : "recipientKindProperty"),
        run: () => onChoose(key) }))} /> : null}
  </span>;
}
