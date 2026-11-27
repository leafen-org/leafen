"use client";

import type { ReactNode } from "react";
import { ArrowRight, CalendarIcon, Pencil, Plus, TextJustifyLeftSmallIcon } from "@/icons/leafen";
import type { AutomationAction, AutomationValueExpression, DbProperty } from "@/lib/types";
import { AutomationActionIcon } from "@/components/automation/AutomationActionIcon";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { PropertyIcon } from "./PropertyIcon";
import { fitAutomationMarks } from "./automationMarkFit";
import { triggerChosen, triggerConditionKind, triggerPropertyOptions } from "./automationTriggerConditions";
import type { AutomationTriggerDraft, EventTriggerConditionDraft } from "./databaseAutomationPanelModel";
import styles from "./AutomationMarks.module.css";

/**
 * A trigger or an action as a list card's mark: Notion's tinted 28px squares
 * (seen 2026-10-24 on a phone, 2026-10-25 at 1280px) — a page added and any
 * property's edit blue, a recurring one's calendar blue, one that names a
 * single option in that option's colour (Stage = Lost red, Stage → Won
 * green), the rest grey. A condition or an edit of a deleted property has
 * none, and "→" shows only between marks on both sides (Notion's, seen
 * 2026-11-11). Past six slots a side ends in a grey "+n" (fitAutomationMarks).
 *
 * A property's condition, on or paused alike (Notion's, seen 2026-11-18 in
 * Chrome at 1440 on four automations): blue where its value is no option
 * (Count = 5, Note = hi, Due edited), an option's colour where it names one
 * (Tags contains A yellow, Stage = Won green), grey where it names several
 * or all of them; one person its face, 26 round and ringed; a checkbox's
 * none.
 *
 * An action's mark is chosen per action as Notion's list card does (its
 * CollectionSettingsAutomationsView, read 2026-11-18): a notification a blue
 * bell, a page added somewhere a blue "+" (none until its database is
 * chosen), a webhook grey braces, "변수 정의" four grey lines (not the equals
 * sign its card and the list of actions draw; seen 2026-11-18 in Chrome at
 * 1440), Slack and the mail their own mark in a bordered box with no ground
 * (Notion's Slack and Gmail; Leafen's mail is its envelope), a page archived
 * none. An edit that sets one property of this database shows that
 * property's mark: a select or status its option's colour, and none when the
 * option is gone or not chosen; a multi-select its one option's colour, grey
 * for several, none for none (seen 2026-11-18 in two tabs: "Stage 지우기" and
 * "Tags 지우기" left the card "+" alone); one person their face, anyone else
 * grey; any other property grey. An edit setting two or more properties
 * shows its own pencil in grey; one setting none, or a property of another
 * database, or a checkbox line still at "선택…", none. Each action shows its
 * own mark, the same one twice too (seen 2026-11-18 in Chrome at 1440: Count
 * set by "속성 편집" and by "페이지 편집" of this database "+ → # #", Done ☑
 * by "속성 편집" beside a "페이지 편집" Done line at "선택…" "+ → ☑").
 */
function Mark({ tone, children }: { tone: string; children: ReactNode }) {
  return <span className={styles.mark} data-tone={tone} aria-hidden="true">{children}</span>;
}

/** Slack's and the mail's mark: their own, 16 in a 1px bordered box rounded 6, no ground (Notion's app mark). */
function AppMark({ children }: { children: ReactNode }) {
  return <span className={styles.app} aria-hidden="true" data-automation-app-mark>{children}</span>;
}

/** The marks a side holds past its slots, "+n" in a grey 28 square (Notion's, seen 2026-11-18). */
function MoreMarks({ count }: { count: number }) {
  return count ? <span className={styles.more} aria-hidden="true" data-automation-mark-more>+{count}</span> : null;
}

/** The one option a condition names, for its mark's colour. */
function oneOptionColor(property: DbProperty | undefined, value: unknown) {
  if (!property || (triggerConditionKind(property) !== "options" && property.type !== "multi_select")) return null;
  const chosen = Array.isArray(value) ? triggerChosen(value) : typeof value === "string" ? [value] : null;
  if (chosen?.length !== 1) return null;
  return triggerPropertyOptions(property).find((option) => option.id === chosen[0])?.color ?? null;
}

/** One person's face where a person property names only them, 26 round in the mark's 28. */
function personFace(key: string, property: DbProperty | undefined, value: unknown) {
  const chosen = property?.type === "person" ? triggerChosen(value) : null;
  return chosen?.length === 1 && typeof chosen[0] === "string"
    ? <span key={key} className={styles.face} aria-hidden="true"><PersonAvatar userId={chosen[0]} size={26} /></span> : null;
}

/** A property condition's mark: its tone, a face for one person, or none for a checkbox's. */
function conditionMark(condition: Extract<EventTriggerConditionDraft, { type: "property_edited" }>, property: DbProperty) {
  const kind = triggerConditionKind(property);
  if (kind === "checkbox") return null;
  const face = personFace(condition.id, property, condition.value);
  if (face) return face;
  const tone = oneOptionColor(property, condition.value) ?? (kind === "options" || kind === "list" ? "gray" : "blue");
  return <Mark key={condition.id} tone={tone}><PropertyIcon property={property} size="small" /></Mark>;
}

/** An edit's mark when it sets one property of this database: its tone or face, or none (see above). */
function propertyEditMark(actionId: string, property: DbProperty, value: AutomationValueExpression): ReactNode {
  const literal = value.kind === "literal" ? value.value : undefined;
  // A checkbox line not yet chosen ("선택…") sets nothing.
  if (property.type === "checkbox" && value.kind === "literal" && literal === null) return null;
  const chosen = Array.isArray(literal) ? triggerChosen(literal) ?? [] : typeof literal === "string" && literal ? [literal] : [];
  // A chosen option that is still there tints the mark, grey without a colour of its own.
  const optionColor = (id: unknown) => {
    const option = triggerPropertyOptions(property).find(({ id: optionId }) => optionId === id);
    return option ? option.color || "gray" : undefined;
  };
  let tone: string | undefined = "gray";
  if (triggerConditionKind(property) === "options") tone = optionColor(chosen[0]);
  else if (property.type === "multi_select") tone = chosen.length === 1 ? optionColor(chosen[0]) : chosen.length ? "gray" : undefined;
  else if (property.type === "person") {
    const face = personFace(actionId, property, chosen);
    if (face) return face;
  }
  return tone ? <Mark key={actionId} tone={tone}><PropertyIcon property={property} size="small" /></Mark> : null;
}

/** An action's list card mark, or none. */
function actionMark(action: AutomationAction, properties: DbProperty[]): ReactNode {
  const own = (tone: string, icon: ReactNode = <AutomationActionIcon type={action.type} />) =>
    <Mark key={action.id} tone={tone}>{icon}</Mark>;
  switch (action.type) {
  case "send_notification": return own("blue");
  case "add_page": return action.databaseId ? own("blue") : null;
  case "send_webhook": return own("gray");
  // A page's icon set reads as a gray face (Notion's, seen 2026-11-21).
  case "set_page_icon": return own("gray");
  case "define_variables": return own("gray", <TextJustifyLeftSmallIcon size="small" />);
  case "send_slack":
  case "send_email":
    return <AppMark key={action.id}><AutomationActionIcon type={action.type} /></AppMark>;
  case "edit_property":
  case "edit_pages": {
    const changes = action.type === "edit_property" ? [action] : action.changes;
    if (changes.length > 1) return own("gray", <AutomationActionIcon type="edit_pages" />);
    const property = changes[0] && properties.find(({ id }) => id === changes[0].propertyId);
    return property ? propertyEditMark(action.id, property, changes[0].value) : null;
  }
  default: return null;
  }
}

/** An automation's triggers, "→" and its actions, as its list card leads with them. */
export function AutomationMarks({ trigger, actions, properties, className }: {
  trigger: AutomationTriggerDraft;
  actions: AutomationAction[];
  properties: DbProperty[];
  className?: string;
}) {
  const triggers = trigger.type === "schedule"
    ? [<Mark key="schedule" tone="blue"><CalendarIcon size="small" /></Mark>]
    : trigger.conditions.flatMap((condition) => {
      if (condition.type === "row_added") return [<Mark key={condition.id} tone="blue"><Plus size="small" /></Mark>];
      if (!condition.propertyId) return [<Mark key={condition.id} tone="blue"><Pencil size="small" /></Mark>];
      const property = properties.find(({ id }) => id === condition.propertyId);
      const mark = property ? conditionMark(condition, property) : null;
      return mark ? [mark] : [];
    });
  const marks = actions.flatMap((action) => {
    const mark = actionMark(action, properties);
    return mark ? [mark] : [];
  });
  const fit = fitAutomationMarks(triggers.length, marks.length);
  return (
    <span className={className ? `${styles.marks} ${className}` : styles.marks}>
      {triggers.slice(0, fit.triggers.shown)}
      <MoreMarks count={fit.triggers.more} />
      {triggers.length && marks.length ? <ArrowRight size="small" className={styles.markArrow} aria-hidden="true" data-automation-mark-arrow /> : null}
      {marks.slice(0, fit.actions.shown)}
      <MoreMarks count={fit.actions.more} />
    </span>
  );
}
