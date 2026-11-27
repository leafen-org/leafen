import { useLayoutEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ArrowLeft, ChevronDown } from "@/icons/leafen";
import type { DbProperty } from "@/lib/types";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { usePopoverAppear } from "@/lib/popoverAppear";
import { AutomationChoicePopover } from "@/components/automation/AutomationChoicePopover";
import { chipStyle } from "./colors";
import { useTriggerConditionChoices, type TriggerConditionValue } from "./PhoneAutomationCondition";
import { triggerOperatorChoices, triggerOperatorLabel, type TriggerOperatorChoice } from "./automationTriggerConditions";
import { AutomationStatusConditionList, type StatusConditionKeys } from "./AutomationStatusConditionList";
import { AutomationCheckboxConditionList } from "./AutomationCheckboxConditionList";
import { RelationConditionPicks } from "./AutomationRelationCondition";
import { ConditionPersonRow, ConditionPicksField } from "./AutomationConditionPicks";
import { useConditionListKeys } from "./useConditionListKeys";
import styles from "./AutomationConditionValuePopover.module.css";
import pickStyles from "./AutomationConditionPicks.module.css";
import { POPOVER_MARGIN } from "@/lib/popoverPlacement";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

const WIDTH = 266;
// "모든 옵션"'s place among an option list's rows for the keys.
const ALL = "\u0000all";

/**
 * A property condition's value on a desktop, as Notion's (seen 2026-10-25 at
 * 1280px): a 266px window where "+ 새 조건" listed the property, or under the
 * condition's card — "← Stage 다음과 동일함 ⌄ … 완료" (no "←" from the card),
 * a search holding the ticked ones as chips ("하나 이상의 옵션을 선택하세요..."
 * while none is ticked, "모든 옵션" while all are — ConditionPicksField), a
 * line, "☐ 모든 옵션" ("−" while only some are ticked), a line and each
 * option's pill ticked; people the same but picked from their faces, no box
 * (ConditionPersonRow);
 * a number's or words' value. The same choices as a phone's screen
 * (useTriggerConditionChoices). A status's has no operator and no search:
 * "← 상태 속성 설정 값: 완료" over its groups and their options
 * (AutomationStatusConditionList, seen 2026-11-11); a checkbox's the same
 * head over its two states, a ✓ after each ticked (AutomationCheckboxConditionList,
 * seen 2026-11-18). A relation's
 * pages, chips in the search over the database's pages (RelationConditionPicks,
 * seen 2026-11-17). ↑, ↓ and Enter light and press an option list's or
 * people's rows (useConditionListKeys); people's "모든 옵션" stays on the lit
 * ground, as Notion's (seen 2026-11-18 in two tabs). Its changes are the condition's as they are made: "완료",
 * Escape and a press outside all keep them (`onClose` does as `onDone`, the
 * caller's), "←" leaves a new condition unmade.
 */
export function AutomationConditionValuePopover({ property, condition, trigger, onBack, onChange, onDone, onClose }: {
  property: DbProperty;
  condition: TriggerConditionValue;
  trigger: HTMLElement | null;
  /** "←" back to the conditions menu, when it was picked there. */
  onBack?(): void;
  onChange(next: TriggerConditionValue): void;
  onDone(): void;
  onClose(): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const ref = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const operatorRef = useRef<HTMLButtonElement>(null);
  const doneRef = useRef<HTMLButtonElement>(null);
  const statusRef = useRef<StatusConditionKeys>(null);
  const status = property.type === "status";
  const [query, setQuery] = useState("");
  const [operators, setOperators] = useState(false);
  const choices = useTriggerConditionChoices(property, condition, onChange);
  const { kind, operator, items, chosen, all, checkbox, states } = choices;
  usePopoverAppear(true, ref, () => trigger);
  const layer = useOverlayLayer(true, { kind: "picker", ref, onEscape: onClose, onOutsidePress: onClose, inside: () => [trigger],
    returnFocusRef: { current: trigger } });
  // Under what opened it, kept on the screen: right under "+ 새 조건"'s row, at the foot of the 8px under a card
  // (7.6 below it, a status's card and words' alike), as Notion's (seen 2026-11-11).
  const gap = onBack ? 0 : 7.6;
  const place = useMemo((): CSSProperties => {
    const rect = trigger?.getBoundingClientRect();
    if (!rect || typeof window === "undefined") return { top: 0, left: 0, width: WIDTH };
    const left = Math.max(POPOVER_MARGIN, Math.min(rect.left, window.innerWidth - WIDTH - POPOVER_MARGIN));
    return { top: rect.bottom + gap, left, width: WIDTH, maxHeight: Math.max(160, window.innerHeight - rect.bottom - 16) };
  }, [trigger, gap]);
  // What takes the keys as the window opens, one rule wherever it opened (judged after the press only, a letter typed then, no
  // script in the page before it, Notion in Chrome 1440): a field asking for words at once — an option list's or a relation's
  // search, a number's value — takes them from "+ 새 조건" and from the card alike; people's search only from their card
  // (from "+ 새 조건" people start under "모든 옵션" and a letter lands nowhere). Words' value never takes them (2026-11-17 in
  // three tabs). With no such field, from "+ 새 조건" the window holds the keys (a status's, a checkbox's, people's, words'),
  // and from a card the head's first button does: the operator (words', "편집됨" with no list), else "완료" (a status's, a
  // checkbox's). Seen 2026-11-15, 2026-11-17 and 2026-11-18 in two tabs; ↑↓ still walk a status's rows from "완료".
  // An operator turning a field up later gives it the keys, words' value aside.
  const fromMenu = Boolean(onBack);
  const opened = useRef(false);
  useLayoutEffect(() => {
    const first = !opened.current;
    opened.current = true;
    const field = kind === "text" || status || (choices.personList && fromMenu) ? null : searchRef.current;
    const head = fromMenu ? ref.current : operatorRef.current ?? doneRef.current;
    (field ?? (first ? head : null))?.focus({ preventScroll: true });
  }, [choices.choosing, choices.typing]);
  const shown = items.filter((item) => item.name.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  const nothing = !all && !(chosen ?? []).length;
  // People picked leave the list for the search's chips; under "모든 옵션" all are listed, dimmed (ConditionPersonRow).
  const listed = choices.personList ? shown.filter((item) => all || !(chosen ?? []).includes(item.id)) : shown;
  const keys = useConditionListKeys({ keys: choices.personList ? listed.map((item) => item.id) : [ALL, ...listed.map((item) => item.id)],
    round: choices.personList, reset: query,
    // A person picked empties the search, as Notion's Enter does (seen 2026-11-18).
    onPress: (key) => {
      if (key === ALL) choices.toggleAll();
      else if (choices.personList) { choices.pick(key); setQuery(""); } else choices.toggle(key);
    },
    onWalk: (key) => ref.current?.querySelector(key === ALL ? "[data-automation-condition-all]"
      : `[data-automation-condition-item="${CSS.escape(key)}"]`)?.closest("label, button")?.scrollIntoView({ block: "nearest" }) });
  // The rows take the keys from the window, its search and "모든 옵션"'s box; a button keeps its own Enter.
  const listKeys = (target: EventTarget) => choices.choosing && kind !== "pages" && !status && !operators
    && (target === ref.current || target === searchRef.current || (target instanceof Element && target.matches("[data-automation-condition-all]")));
  if (typeof document === "undefined") return null;
  return createPortal(
    <div ref={ref} role="dialog" aria-label={property.name} tabIndex={-1} className={styles.popover} style={{ ...place, zIndex: layer.zIndex }}
      onKeyDown={(event) => {
        if (listKeys(event.target)) keys.keyDown(event);
        // A status's or a checkbox's rows take ↑↓ wherever the focus is in the window, and Enter or Space unless a button holds it.
        if ((status || checkbox) && !operators && (event.key === "ArrowDown" || event.key === "ArrowUp" || !(event.target instanceof HTMLButtonElement))) {
          statusRef.current?.keyDown(event);
        }
        if (!event.defaultPrevented) layer.onKeyDown(event);
      }}
      data-automation-condition-value={property.id}>
      <div className={styles.head}>
        {onBack ? (
          <button type="button" className={styles.back} aria-label={t("common:actions.back")} onClick={onBack}>
            <ArrowLeft size="small" aria-hidden="true" />
          </button>
        ) : null}
        {status || checkbox ? <span className={styles.headWords}>{t(status ? "databaseView:automationTriggerStatusHead"
          : "databaseView:automationTriggerCheckbox.head", { name: property.name })}</span>
          : <span className={styles.name}>{property.name}</span>}
        {checkbox || status ? null : (
          <button ref={operatorRef} type="button" className={styles.operator} aria-haspopup="menu" aria-expanded={operators}
            aria-label={t("databaseView:automationTriggerOperatorFor", { name: property.name })} data-automation-condition-operator={operator}
            onClick={() => setOperators(true)}>
            {triggerOperatorLabel(t, property, operator)}<ChevronDown size="tiny" aria-hidden="true" />
          </button>
        )}
        <button ref={doneRef} type="button" className={styles.done} onClick={onDone} data-automation-condition-done>{t("common:actions.done")}</button>
      </div>
      {status ? (
        <AutomationStatusConditionList ref={statusRef} property={property} condition={condition} onChange={onChange}
          label={t("databaseView:automationTriggerStatusHead", { name: property.name })} />
      ) : checkbox ? (
        <AutomationCheckboxConditionList ref={statusRef} property={property} states={states} onToggle={choices.toggleState}
          label={t("databaseView:automationTriggerCheckbox.head", { name: property.name })} />
      ) : kind === "pages" && choices.choosing ? (
        <RelationConditionPicks property={property} value={condition.value} inputRef={searchRef} onChange={(value) => onChange({ ...condition, value })} />
      ) : choices.choosing ? (
        <>
          <div className={styles.searchRow}>
            <ConditionPicksField choices={choices} query={query} onQuery={setQuery} inputRef={searchRef} label={t("databaseView:automationTriggerAllOptions")}
              placeholder={t(choices.personList ? "databaseView:automationTriggerSearchPeople"
                : nothing ? "databaseView:automationTriggerPickOptions" : "databaseView:automationTriggerAllOptions")} />
          </div>
          <div className={styles.rule} role="separator" />
          <div className={styles.rows}>
            <label className={styles.row} data-lit={choices.personList || keys.lit === ALL || undefined}
              onPointerMove={choices.personList || keys.lit === ALL ? undefined : () => keys.light(ALL)}>
              <input type="checkbox" checked={choices.allTicked} ref={choices.allBox} onChange={choices.toggleAll} data-automation-condition-all />
              <span>{t("databaseView:automationTriggerAllOptions")}</span>
            </label>
            <div className={styles.rule} role="separator" />
            {choices.personList ? (
              <div className={pickStyles.people}>
                {listed.map((item) => (
                  <ConditionPersonRow key={item.id} id={item.id} name={item.name} dimmed={all} lit={keys.lit === item.id} onLight={keys.light}
                    onPick={choices.toggle} />
                ))}
              </div>
            ) : listed.map((item) => (
              <label key={item.id} className={styles.row} data-automation-condition-item={item.id} data-lit={keys.lit === item.id || undefined}
                onPointerMove={keys.lit === item.id ? undefined : () => keys.light(item.id)}>
                <input type="checkbox" checked={all || (chosen ?? []).includes(item.id)} onChange={() => choices.toggle(item.id)} />
                {item.color !== undefined || choices.pills
                  ? <span className={styles.pill} style={chipStyle(item.color)} data-status-pill={property.type === "status" ? "" : undefined}>{item.name}</span>
                  : <span>{item.name}</span>}
              </label>
            ))}
          </div>
        </>
      ) : choices.typing ? (
        <div className={styles.searchRow}>
          <input ref={searchRef} className={styles.search} inputMode={kind === "number" ? "decimal" : undefined} value={choices.typed}
            placeholder={t("databaseView:automationTriggerValue")} aria-label={t("databaseView:automationTriggerValue")} data-automation-condition-typed
            onChange={(event) => choices.type(event.currentTarget.value)}
            onKeyDown={(event) => { if (fieldEnter(event)) { event.preventDefault(); onDone(); } }} />
        </div>
      ) : null}
      {operators ? (
        <AutomationChoicePopover label={t("databaseView:automationTriggerOperatorFor", { name: property.name })} trigger={operatorRef.current}
          width={180} onClose={() => setOperators(false)}
          choices={triggerOperatorChoices(property).map((key) => ({ key, text: triggerOperatorLabel(t, property, key) }))}
          onPick={(key) => choices.pickOperator(key as TriggerOperatorChoice)} />
      ) : null}
    </div>,
    document.body,
  );
}
