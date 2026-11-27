import { SwitchInput } from "@/components/Switch";
import { useWaitingNote } from "@/lib/pendingChoice";
import { writeUncertainMark } from "@/lib/settingsErrors";
import { useEffect, useRef, useState, type Dispatch, type ReactNode, type RefObject, type SetStateAction } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ChevronDown, LockIcon, X } from "@/icons/leafen";
import type { DatabaseAutomationScope } from "@/databaseAutomationWire.generated";
import type { AutomationAction, DbProperty, DbView, Page } from "@/lib/types";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { AutomationActionEditor, automationEditorActionsValid, type AutomationEditorSurface } from "@/components/automation/AutomationActionEditor";
import { AutomationFloatingMenus } from "@/components/automation/AutomationActionMenus";
import { AutomationChoicePopover } from "@/components/automation/AutomationChoicePopover";
import { ViewIcon, viewDisplayName } from "./databaseViewShared";
import { automationViewFilterCount, databaseAutomationEditorBounds, type AutomationTriggerDraft } from "./databaseAutomationPanelModel";
import { AutomationDialogConditions } from "./AutomationDialogConditions";
import { triggerConditions } from "./automationConditionEditing";
import styles from "./DatabaseAutomationDialog.module.css";

/**
 * An automation's editor on a desktop, as Notion's dialog (seen 2026-10-24
 * at 1280px): 500×600 in the middle, its name in 17px over where it applies
 * ("〈DB|보기〉 ⌄ 의 모든 페이지에 적용", a menu of the database and its
 * views) and a line, a round "×"; then "다음 경우에 사용:" and its
 * conditions, a joining line, "작업" and its actions; "취소" and a blue
 * "활성화" (an open one's "완료") at its foot. An open one has its "활성"
 * switch under its name, as on a phone. The panel it was opened from stays
 * under it, and takes the keys back in its search when it closes, as
 * Notion's (seen 2026-11-07).
 */
export function DatabaseAutomationDialog({ isNew, changed, readOnly, broken, invalid = false, busy, error, databaseName, databaseIcon, draftActions, draftName, namePlaceholder,
  draftSurface, draftTrigger, draftScope, setDraftScope, active, onToggleActive, editorPages, properties, triggerProperties, editsItsPage, serializedDraftTrigger,
  triggerPropertyLabels, views, onClose, onSave, setDraftActions, setDraftName, setDraftTrigger, selectSchedule, returnFocusRef, restoresFocus }: {
  isNew: boolean;
  /** Whether the draft differs from where it began: an open one unchanged has nothing to save. */
  changed: boolean;
  readOnly: boolean;
  broken: boolean;
  /** An open one as saved names what the database no longer has: its switch hides until then (Notion's, seen 2026-11-11). */
  invalid?: boolean;
  busy: boolean;
  error?: string;
  databaseName: string;
  /** The database's own icon, before its name where it applies; none without one (Notion's, seen 2026-11-14). */
  databaseIcon?: ReactNode;
  draftActions: AutomationAction[];
  draftName: string;
  /** What an unnamed one reads as: its maker's, in grey. */
  namePlaceholder?: string;
  draftSurface: AutomationEditorSurface;
  draftTrigger: AutomationTriggerDraft;
  draftScope: DatabaseAutomationScope;
  setDraftScope(scope: DatabaseAutomationScope): void;
  /** An open one's state, turned at once; null for a new one. */
  active: boolean | null;
  onToggleActive?(): void;
  editorPages: Page[];
  properties: DbProperty[];
  /** The properties a condition may name; unset, any it lists. */
  triggerProperties?: DbProperty[];
  /** Whether a step may edit the page it ran for: People's may not. */
  editsItsPage?: boolean;
  serializedDraftTrigger: object | null;
  triggerPropertyLabels: string[];
  views: DbView[];
  /** "×" and "취소": the panel asks first when there are changes. */
  onClose(): void;
  onSave(): Promise<void>;
  setDraftActions: Dispatch<SetStateAction<AutomationAction[]>>;
  setDraftName: Dispatch<SetStateAction<string>>;
  setDraftTrigger: Dispatch<SetStateAction<AutomationTriggerDraft>>;
  selectSchedule(): void;
  /** Where the keys go back when it closes: the panel's search under it. */
  returnFocusRef?: RefObject<HTMLElement | null>;
  /** Whether they go back there as it closes: not after a new one is made from the list. */
  restoresFocus?: () => boolean;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const ref = useRef<HTMLDivElement>(null);
  const [scopeFrom, setScopeFrom] = useState<HTMLElement | null>(null);
  const [warned, setWarned] = useState(false);
  // A new one's name starts empty, its maker's in grey with the caret in it; left empty, it reads its maker's as its
  // words, as Notion's (seen 2026-11-15 in two tabs) — still saved unnamed, so nothing has changed. One saved
  // unnamed opens with its maker's as its words, white and bold (Notion's, seen 2026-11-15 twice).
  const [nameFilled, setNameFilled] = useState(!isNew);
  const shownName = draftName || (nameFilled ? namePlaceholder ?? "" : "");
  // Presses of a dimmed "활성화": each turns what is missing red (AutomationActionEditor's revealProblems).
  const [revealed, setRevealed] = useState(0);
  // Its shade, drawn before it, stays pressable while the page behind is inert.
  const layer = useOverlayLayer(true, { kind: "dialog", ref, onEscape: onClose, returnFocusRef, shouldRestoreFocus: restoresFocus,
    modalOwned: () => [ref.current?.previousElementSibling] });
  // A condition on a deleted property says to fix it at once, beside the button it holds back (Notion's, seen 2026-11-11).
  const conditionGone = triggerConditions(draftTrigger).some((condition) => condition.type === "property_edited" && condition.propertyId
    && !properties.some(({ id }) => id === condition.propertyId));
  const valid = (isNew || changed) && !broken && Boolean(serializedDraftTrigger)
    && automationEditorActionsValid(draftActions, draftSurface, databaseAutomationEditorBounds, properties) && !busy;
  // A save the server hasn't answered in ten seconds says so beside its button.
  const waitingNote = useWaitingNote(busy);
  // Why a save didn't go is said at the end of the steps: it comes into
  // sight there (2026-11-06: it stood below the scroll, and the button came
  // back with nothing said).
  const errorRef = useRef<HTMLParagraphElement>(null);
  useEffect(() => { if (error) errorRef.current?.scrollIntoView({ block: "nearest" }); }, [error]);
  const scopeName = draftScope.type === "view"
    ? (() => { const view = views.find(({ id }) => id === draftScope.viewId); return view ? viewDisplayName(view) : databaseName; })()
    : databaseName;
  const [scopeBefore = "", scopeAfter = ""] = t("databaseView:automationScopeAllPages", { name: "\u0001" }).split("\u0001");
  if (typeof document === "undefined") return null;
  return createPortal(
    <>
      {/* A press beside it closes it as "취소" does (asking first when there are changes) and does nothing more: a
          view's chosen cell or ticked rows stay (keepsViewChoice), as Notion's (seen 2026-11-27 in Chrome at 1440). */}
      <div className={styles.backdrop} style={layer.zIndex === undefined ? undefined : { zIndex: layer.zIndex - 1 }} aria-hidden="true"
        onClick={onClose} />
      <div ref={ref} className={styles.dialog} role="dialog" aria-modal="true"
        aria-label={t(isNew ? "databaseView:automationCreate" : "databaseView:automationEditTitle")}
        style={layer.zIndex === undefined ? undefined : { zIndex: layer.zIndex }} data-automation-dialog onKeyDown={layer.onKeyDown}>
        <button type="button" className={styles.close} aria-label={t("databaseView:closeAutomations")} onClick={onClose}>
          <X size="tiny" aria-hidden="true" />
        </button>
        <header className={styles.head}>
          <input className={styles.name} value={shownName} maxLength={100} readOnly={readOnly} autoFocus={isNew}
            aria-label={t("databaseView:automationName")} placeholder={namePlaceholder} onBlur={() => setNameFilled(!draftName)}
            onChange={(event) => { setDraftName(event.currentTarget.value); setNameFilled(false); }} />
          {/* A saved automation's switch stands on its scope's line, "활성" and a
              line before it, as Notion's (seen 2026-11-09). */}
          <div className={styles.scopeLine}>
          {active !== null && !invalid ? (
            <label className={styles.activeRow}>
              <SwitchInput checked={active} disabled={readOnly || busy || !onToggleActive} data-automation-active
                onChange={() => onToggleActive?.()} />
              <span>{t("databaseView:automationActive")}</span>
            </label>
          ) : null}
          <p className={styles.scope} data-automation-scope-row>
            {draftTrigger.type === "schedule" ? <span className={styles.scopeName}>{t("databaseView:automationScopeDatabase", { name: databaseName })}</span> : <>
              {scopeBefore}
              <button type="button" className={styles.scopeButton} disabled={readOnly} data-automation-scope-pick
                onClick={(event) => setScopeFrom(event.currentTarget)}>
                <span className={styles.scopeName}>{scopeName}</span><ChevronDown size="tiny" aria-hidden="true" />
              </button>
              {scopeAfter}
            </>}
          </p>
          </div>
        </header>
        <div className={styles.body}>
          {readOnly ? (
            <p className={styles.readOnly} data-automation-read-only>
              <LockIcon size="small" aria-hidden="true" />{t("databaseView:automationReadOnly")}
            </p>
          ) : null}
          <AutomationDialogConditions draftTrigger={draftTrigger} setDraftTrigger={setDraftTrigger} properties={properties} offered={triggerProperties}
            triggerPropertyLabels={triggerPropertyLabels} readOnly={readOnly} selectSchedule={selectSchedule} />
          <span className={styles.line} aria-hidden="true" />
          <section className={styles.section} aria-label={t("databaseView:automationAction")}>
            <p className={styles.sectionLabel}>{t("databaseView:automationAction")}</p>
            <fieldset className={styles.actions} disabled={readOnly}>
              <AutomationFloatingMenus.Provider value={true}>
              <AutomationActionEditor surface={draftSurface} bounds={databaseAutomationEditorBounds} actions={draftActions}
                properties={properties} pages={editorPages} views={views} editsItsPage={editsItsPage} newWhenEmpty warnEarlierOnAdd revealProblems={revealed} onWarnings={setWarned}
                onChange={setDraftActions} />
              </AutomationFloatingMenus.Provider>
            </fieldset>
          </section>
          {error ? <p ref={errorRef} className={styles.error} role="alert" {...writeUncertainMark(error)}>{error}</p> : null}
        </div>
        <footer className={styles.foot}>
          <button type="button" className={styles.cancel} onClick={onClose}>{t("common:actions.cancel")}</button>
          {readOnly ? null : (
            <span className={styles.footEnd}>
              {/* What an unfinished action warns of, beside the button it holds back, as Notion's (seen 2026-11-02);
                  an open one that runs says so as a new one does (seen 2026-11-08). */}
              {warned || conditionGone ? <span className={styles.footNote} data-automation-fix-errors>
                {t(isNew || active ? "databaseView:automationFixToActivate" : "databaseView:automationFixToSave")}</span>
                : waitingNote ? <span className={styles.footNote} role="status" data-automation-waiting>{waitingNote}</span> : null}
              {/* "활성화" for a new one, "완료" for one already made, as Notion's (seen 2026-11-07); dimmed
                  while it can't save, and pressed then it marks what is missing in red (seen 2026-11-08). */}
              <button type="button" className={styles.save} disabled={busy} aria-disabled={!valid || undefined} data-automation-save
                onClick={() => { if (valid) void onSave(); else setRevealed((count) => count + 1); }}>
                {t(isNew ? "databaseView:automationActivate" : "common:actions.done")}
              </button>
            </span>
          )}
        </footer>
        {scopeFrom ? (
          // Right under "<source> ⌄", 230px wide (min 180, at most the window less 24), its search 12px in and the
          // rows 12px under it, a line 4px from the rows on each side: Notion's, People's and a database's alike (seen 2026-11-15).
          <AutomationChoicePopover label={t("databaseView:automationScopeTitle")} searchPlaceholder={t("databaseView:automationScopeSearch")}
            width={230} flush tightRules trigger={scopeFrom} onClose={() => setScopeFrom(null)}
            choices={[
              { key: "database", text: databaseName, icon: databaseIcon, detail: t("databaseView:automationScopeWhole"), chosen: draftScope.type === "database" },
              // The views under a line, as Notion's (seen 2026-11-14).
              ...views.map((view, index) => ({ key: `view:${view.id}`, text: viewDisplayName(view), rule: index === 0,
                icon: <ViewIcon view={{ type: view.type, config: view.config as Record<string, unknown> | undefined }} />,
                detail: automationViewFilterCount(view) ? t("databaseView:automationScopeFilters", { count: automationViewFilterCount(view) })
                  : t("databaseView:automationScopeView"),
                chosen: draftScope.type === "view" && draftScope.viewId === view.id })),
            ]}
            onPick={(key) => setDraftScope(key === "database" ? { type: "database" } : { type: "view", viewId: key.slice("view:".length) })} />
        ) : null}
      </div>
    </>,
    document.body,
  );
}
