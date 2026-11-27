"use client";

import { DatabaseAutomationReaderNote } from "./DatabaseAutomationReaderNote";
import { SwitchInput } from "@/components/Switch";
import { useState, type Dispatch, type ReactNode, type SetStateAction } from "react";
import { useTranslation } from "react-i18next";
import {
  ArrowLeft, ChevronDown, Copy, DotsHorizontal, LightningIcon, PauseIcon, Pencil, Plus, RepeatIcon, Trash, XCircleIcon,
} from "@/icons/leafen";
import { WarningFillIcon } from "@/icons/leafen/automationIcons";
import { confirmAction } from "@/components/ConfirmActionDialog";
import type { DatabaseAutomationScope } from "@/databaseAutomationWire.generated";
import type { AutomationAction, DatabaseAutomationDefinition, DbProperty, DbView, Page } from "@/lib/types";
import {
  AutomationActionEditor,
  automationEditorActionsValid,
  type AutomationEditorSurface,
} from "@/components/automation/AutomationActionEditor";
import { AutomationPhoneSheets } from "@/components/automation/AutomationActionMenus";
import { GonePropertyMark, GonePropertyMenu, GonePropertyWords } from "@/components/automation/AutomationGoneProperty";
import { PhoneSheet, PhoneSheetHeadButton, PhoneSheetMenu, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { PhoneChoiceSheet } from "./DatabasePhonePickers";
import { PropertyIcon } from "./PropertyIcon";
import { viewDisplayName } from "./databaseViewShared";
import { defaultTriggerCondition, settledTriggerCondition, triggerConditionKind } from "./automationTriggerConditions";
import { scheduleChipAround, scheduleChipLabel, withSchedulePreset } from "./automationScheduleModel";
import { PhoneTriggerConditionScreen, TriggerConditionSentence, type TriggerConditionValue } from "./PhoneAutomationCondition";
import { PhoneSchedulePresets, PhoneScheduleSheet } from "./PhoneAutomationSchedule";
import { hasTriggerCondition, modeHeadWords, triggerConditions, triggerNamesProperties, withAnyPropertyEdited, withoutTriggerCondition,
  triggerSaysMode, withConditionValue, withTriggerCondition, type ConditionKind } from "./automationConditionEditing";
import { PhoneAutomationScopeScreen } from "./PhoneAutomationScope";
import { AutomationMarks } from "./AutomationMarks";
import {
  databaseAutomationEditorBounds,
  isDatabaseAutomationListedProperty,
  type AutomationTriggerDraft,
} from "./databaseAutomationPanelModel";
import styles from "./DatabaseAutomationPhone.module.css";
import chipStyles from "./AutomationModeChip.module.css";
import cardStyles from "./AutomationConditionCard.module.css";
import "./automationPhoneSheet.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { PhoneSheetSearch } from "@/components/shell/PhoneSheetSearch";

/**
 * A database's automations in a phone's sheet, as Notion's (seen 2026-10-24
 * at 375px): "‹ 자동화", a search, a white 10px card per automation — its
 * triggers' and actions' marks, its name with "⏸ 중단됨" after it when off,
 * and its sentence, "⋯" for "사용 중", "편집", "복제" and "삭제" — and a blue
 * "+ 새 자동화". Notion's "ⓘ" and its "트리거 반복 시도 ↗" card lead to its
 * help centre, left out.
 */
export function PhoneAutomationList({ automations, loading, readOnly, busy, error, triggerOf, actionsOf, statusOf, summaryOf, nameOf,
  properties, onNew, onOpen, onToggle, onDelete, onDuplicate, onClose, onBack }: {
  automations: DatabaseAutomationDefinition[];
  /** Its name, or its maker's for one left unnamed. */
  nameOf(automation: DatabaseAutomationDefinition): string;
  loading: boolean;
  readOnly: boolean;
  busy: boolean;
  error: string;
  triggerOf(automation: DatabaseAutomationDefinition): AutomationTriggerDraft;
  actionsOf(automation: DatabaseAutomationDefinition): AutomationAction[];
  /** A word on one that isn't running ("중단됨", "유효하지 않음"), else nothing. */
  statusOf(automation: DatabaseAutomationDefinition): { label: string; invalid: boolean } | null;
  summaryOf(automation: DatabaseAutomationDefinition): string;
  properties: DbProperty[];
  onNew?(): void;
  onOpen(automation: DatabaseAutomationDefinition): void;
  onToggle(automation: DatabaseAutomationDefinition): void;
  onDelete(automation: DatabaseAutomationDefinition): void;
  onDuplicate(automation: DatabaseAutomationDefinition): void;
  onClose(): void;
  /** "‹" back where it opened from; without it, none (a touch screen wider than a phone's, from "⚡"). */
  onBack?(): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const [query, setQuery] = useState("");
  const [menu, setMenu] = useState<DatabaseAutomationDefinition | null>(null);
  const shown = automations.filter((automation) => nameOf(automation).toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  const choose = (run: (automation: DatabaseAutomationDefinition) => void) => () => { const automation = menu; setMenu(null); if (automation) run(automation); };
  return (
    <PhoneSheet title={t("databaseView:automations")} onClose={onClose} onBack={onBack} data={{ "data-phone-automations": "" }}>
      <label className={styles.search}>
        <input value={query} placeholder={t("databaseView:automationSearch")} aria-label={t("databaseView:automationSearch")}
          onChange={(event) => setQuery(event.currentTarget.value)} />
      </label>
      {loading ? <p className={styles.note}>{t("databaseView:loadingAutomations")}</p> : null}
      {!loading && !automations.length ? readOnly ? <DatabaseAutomationReaderNote /> : <p className={styles.note}>{t("databaseView:noAutomations")}</p> : null}
      {shown.map((automation) => {
        const status = statusOf(automation);
        return (
          <article key={automation.id} className={styles.listCard} data-phone-automation={automation.id}>
            <button type="button" className={styles.listOpen} disabled={busy}
              aria-label={t(readOnly ? "databaseView:viewAutomationNamed" : "databaseView:editAutomationNamed", { name: nameOf(automation) })}
              onClick={() => onOpen(automation)}>
              <AutomationMarks trigger={triggerOf(automation)} actions={actionsOf(automation)} properties={properties} className={styles.marks} />
              <span className={styles.listHead}>
                <strong className={styles.listName}>{nameOf(automation)}</strong>
                {status ? <span className={styles.listStatus} data-automation-status data-invalid={status.invalid || undefined}>
                  {status.invalid ? <WarningFillIcon size="micro" aria-hidden="true" /> : <PauseIcon size="micro" aria-hidden="true" />}{status.label}
                </span> : null}
              </span>
              <span className={styles.listSummary} data-automation-summary>{summaryOf(automation)}</span>
            </button>
            {readOnly ? null : (
              <button type="button" className={styles.dots} disabled={busy} aria-label={t("databaseView:automationMenuFor", { name: nameOf(automation) })}
                aria-haspopup="menu" onClick={() => setMenu(automation)}>
                <DotsHorizontal size="regular" aria-hidden="true" />
              </button>
            )}
          </article>
        );
      })}
      {onNew ? (
        <button type="button" className={styles.newButton} disabled={loading || busy} onClick={onNew} data-phone-automation-new>
          <Plus size="small" aria-hidden="true" />{t("databaseView:newAutomation")}
        </button>
      ) : null}
      {error ? <p className={styles.error} role="alert">{error}</p> : null}
      {menu ? (
        <PhoneSheetMenu label={t("databaseView:automationMenuFor", { name: menu.name })} onClose={() => setMenu(null)}>
          <label className={`${sheetStyles.row} ${styles.switchRow}`}>
            <span className={sheetStyles.rowIcon} aria-hidden="true"><LightningIcon size="wide" /></span>
            <span className={sheetStyles.rowLabel}>{t("databaseView:automationInUse")}</span>
            <SwitchInput checked={menu.status === "active"} data-phone-automation-toggle
              onChange={choose(onToggle)} />
          </label>
          <PhoneSheetRow icon={<Pencil size="regular" />} label={t("databaseView:edit")} more={false}
            data={{ "data-phone-automation-edit": "" }} onPress={choose(onOpen)} />
          <PhoneSheetRow icon={<Copy size="regular" />} label={t("databaseView:automationDuplicate")} more={false}
            data={{ "data-phone-automation-duplicate": "" }} onPress={choose(onDuplicate)} />
          <PhoneSheetRow icon={<Trash size="regular" />} label={t("common:actions.delete")} danger more={false}
            data={{ "data-phone-automation-delete": "" }} onPress={choose(onDelete)} />
        </PhoneSheetMenu>
      ) : null}
    </PhoneSheet>
  );
}

type Picking = { kind: "add" } | { kind: "menu"; id: string } | { kind: "mode" } | { kind: "scope" }
  /** A deleted property's card pressed: "속성 복원" and "조건 삭제" in a sheet. */
  | { kind: "gone"; id: string; propertyId: string; anchor: HTMLElement }
  | { kind: "presets" } | { kind: "schedule" }
  | { kind: "condition"; id?: string; propertyId: string; draft: TriggerConditionValue } | null;

/**
 * An automation's editor in a phone's sheet, as Notion's "자동화 생성" (seen
 * 2026-10-24 at 375px): "←" and a blue "저장" in its head; on white its name
 * (with a clear), an open one's "활성" switch (at once, as Notion's), where it
 * applies ("〈DB|보기〉 ⌄ 의 모든 페이지에 적용", or a recurring one's "〈DB〉에
 * 적용"), "다음 경우에 사용:" (with two or more, "〈모든|하나라도 ⌄〉 조건 발생
 * 시") over a card per condition with "⋯", "+ 새 조건"/"+ 조건 추가" that lists
 * the conditions at the bottom, a line down to "작업", and the actions' cards
 * with their own "+ 새 작업". A condition on a deleted property is the
 * desktop's red card, its press "속성 복원" and "조건 삭제" in a sheet; one
 * with no value to ask ("마감일 속성 편집") opens nothing (Notion's, seen
 * 2026-11-11 at 402px).
 */
export function PhoneAutomationEditor({ isNew, changed, readOnly, broken, busy, databaseName, databaseIcon, draftActions, draftName, namePlaceholder, draftSurface, draftTrigger,
  draftScope, setDraftScope, active, onToggleActive, editorPages, properties, triggerProperties = properties, editsItsPage, serializedDraftTrigger, triggerPropertyLabels, views, onBack, onSave,
  setDraftActions, setDraftName, setDraftTrigger, selectSchedule }: {
  isNew: boolean;
  /** Whether the draft differs from where it began: an automation opened unchanged has nothing to save, as Notion's. */
  changed: boolean;
  readOnly: boolean;
  broken: boolean;
  busy: boolean;
  databaseName: string;
  /** The database's own icon in "적용할 페이지 위치". */
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
  /** The properties "+ 조건 추가" offers: People's four, else all it lists. */
  triggerProperties?: DbProperty[];
  /** Whether a step may edit the page it ran for: People's may not. */
  editsItsPage?: boolean;
  serializedDraftTrigger: object | null;
  triggerPropertyLabels: string[];
  views: DbView[];
  onBack(): void;
  onSave(): Promise<void>;
  setDraftActions: Dispatch<SetStateAction<AutomationAction[]>>;
  setDraftName: Dispatch<SetStateAction<string>>;
  setDraftTrigger: Dispatch<SetStateAction<AutomationTriggerDraft>>;
  /** "매...": the trigger becomes a schedule still to be picked. */
  selectSchedule(): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const [picking, setPicking] = useState<Picking>(null);
  const [query, setQuery] = useState("");
  // A new one's name field holds its maker's name as its words, ⓧ clearing
  // them to the grey stand-in (Notion's phone, seen 2026-11-09 in the app and
  // 2026-11-15 at 411 on the web); left so, it is saved unnamed and reads as
  // its maker's still. One saved unnamed opens so again, black with its ⓧ
  // (Notion's phone and iPad, seen 2026-11-15).
  const [nameCleared, setNameCleared] = useState(false);
  const shownName = draftName || (!nameCleared && !readOnly ? namePlaceholder ?? "" : "");
  const close = () => { setPicking(null); setQuery(""); };
  const conditions = triggerConditions(draftTrigger);
  const has = (condition: ConditionKind) => hasTriggerCondition(draftTrigger, condition);
  const labelOf = (propertyId: string) => triggerPropertyLabels[properties.findIndex(({ id }) => id === propertyId)] ?? t("databaseView:untitled");
  const valid = (isNew || changed) && !broken && Boolean(serializedDraftTrigger)
    && automationEditorActionsValid(draftActions, draftSurface, databaseAutomationEditorBounds, properties) && !busy;
  const addCondition = (condition: ConditionKind) => setDraftTrigger((current) => withTriggerCondition(current, condition));
  const removeCondition = (id: string) => setDraftTrigger((current) => withoutTriggerCondition(current, id));
  // What "+ 조건 추가" offers: "편집된 모든 속성" first, a page added once,
  // "매..." while there is nothing else, and each property not yet a
  // condition, "›" where it can say what its edit leaves (Notion's, seen 2026-10-24).
  const choices = [
    ...(!has({ type: "property_edited", propertyId: "" }) && draftTrigger.type === "events"
      ? [{ key: "any_edit", icon: <Pencil size="regular" />, label: t("databaseView:automationAnyPropertyEdited"), more: false }] : []),
    ...(!has({ type: "row_added" }) ? [{ key: "row_added", icon: <Plus size="regular" />, label: t("databaseView:automationPageAddedDone"), more: false }] : []),
    ...(!conditions.length && draftTrigger.type === "events" ? [{ key: "schedule", icon: <RepeatIcon size="regular" />, label: t("databaseView:automationEvery"), more: false }] : []),
    ...triggerProperties.filter((property) => isDatabaseAutomationListedProperty(property) && !has({ type: "property_edited", propertyId: property.id }))
      .map((property) => ({ key: `property:${property.id}`, icon: <PropertyIcon property={property} size="regular" />, label: labelOf(property.id),
        more: triggerConditionKind(property) !== "none" })),
  ].filter((choice) => choice.label.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  // Its marks grey, as Notion's (seen 2026-11-11 at 402px: rgb(142, 139, 134)).
  const choiceMark = (icon: ReactNode, key: string) => <span className={styles.choiceMark} data-property={key.startsWith("property:") || undefined}>{icon}</span>;
  const pick = async (key: string) => {
    if (key === "schedule") {
      // "매..." asks its "주기" at once, as Notion's.
      selectSchedule();
      setQuery("");
      setPicking({ kind: "presets" });
      return;
    } else if (key === "row_added") addCondition({ type: "row_added" });
    else if (key === "any_edit") {
      // Any property's edit takes the place of each property's, once asked (Notion's, seen 2026-10-24).
      const others = triggerNamesProperties(draftTrigger);
      close();
      if (others && !await confirmAction({ message: t("databaseView:automationReplaceTitle"), detail: t("databaseView:automationReplaceDetail"),
        confirmLabel: t("databaseView:automationReplace"), danger: true })) return;
      setDraftTrigger((current) => withAnyPropertyEdited(current));
      return;
    } else {
      const property = properties.find(({ id }) => id === key.slice("property:".length));
      // A property that can say what its edit leaves asks it first, on a screen of its own.
      if (property && triggerConditionKind(property) !== "none") {
        setQuery("");
        setPicking({ kind: "condition", propertyId: property.id, draft: defaultTriggerCondition(property) });
        return;
      }
      addCondition({ type: "property_edited", propertyId: key.slice("property:".length) });
    }
    close();
  };
  const saveCondition = (pickingCondition: Extract<Picking, { kind: "condition" }>) => {
    const { id, propertyId, draft } = pickingCondition;
    const settled = settledTriggerCondition(properties.find((property) => property.id === propertyId), draft);
    setDraftTrigger((current) => withConditionValue(current, id, propertyId, settled));
    close();
  };
  const canAdd = draftTrigger.type === "events" && (!has({ type: "row_added" }) || triggerProperties.some((property) => !has({ type: "property_edited", propertyId: property.id })));
  const card = (key: string, icon: ReactNode, label: ReactNode, onPress?: (element: HTMLElement) => void, gone = false) => (
    <div key={key} className={styles.card} data-phone-automation-condition={key}>
      <div className={styles.cardHead}>
        <span className={styles.cardIcon} aria-hidden="true">{icon}</span>
        {onPress && !readOnly
          ? <button type="button" className={styles.cardLabel} data-gone={gone || undefined} onClick={(event) => onPress(event.currentTarget)}>{label}</button>
          : <span className={styles.cardLabel} data-gone={gone || undefined}>{label}</span>}
        {readOnly ? null : (
          <button type="button" className={`${styles.dots} ${cardStyles.dots}`} aria-label={t("databaseView:automationConditionMenu")} aria-haspopup="menu"
            onClick={() => setPicking({ kind: "menu", id: key })}>
            <DotsHorizontal size="regular" aria-hidden="true" />
          </button>
        )}
      </div>
    </div>
  );
  const [modeBefore, modeAfter] = modeHeadWords(t);
  const head = triggerSaysMode(draftTrigger) ? (
    <p className={styles.sectionLabel}>
      {modeBefore}
      <button type="button" className={chipStyles.modeChip} disabled={readOnly} onClick={() => setPicking({ kind: "mode" })} data-phone-automation-mode>
        {t(draftTrigger.type === "events" && draftTrigger.mode === "all" ? "databaseView:automationModeAll" : "databaseView:automationModeAny")}
        <ChevronDown size="small" trim glyphWidth={9.88} aria-hidden="true" />
      </button>
      {modeAfter}
    </p>
  ) : <p className={styles.sectionLabel}>{t("databaseView:automationUseWhen")}</p>;
  // "Color cards ⌄ 의 모든 페이지에 적용": the database or the view, picked on a screen.
  const scopeName = draftScope.type === "view" ? (() => { const view = views.find(({ id }) => id === draftScope.viewId); return view ? viewDisplayName(view) : databaseName; })() : databaseName;
  const [scopeBefore = "", scopeAfter = ""] = t("databaseView:automationScopeAllPages", { name: "\u0001" }).split("\u0001");
  const chipAround = scheduleChipAround(t);
  return (
    <PhoneSheet title={t(isNew ? "databaseView:automationCreate" : "databaseView:automationEditTitle")} onClose={onBack}
      start={<PhoneSheetHeadButton label={t("common:actions.back")} onPress={onBack}><ArrowLeft size="regular" aria-hidden="true" /></PhoneSheetHeadButton>}
      action={readOnly ? undefined : { label: t("common:actions.save"), disabled: !valid, onPress: () => void onSave() }}
      plain data={{ "data-phone-automation-editor": "" }}>
      <div className={styles.editor}>
        <label className={styles.name}>
          <input value={shownName} maxLength={100} readOnly={readOnly} aria-label={t("databaseView:automationName")} placeholder={namePlaceholder}
            onChange={(event) => { setDraftName(event.currentTarget.value); setNameCleared(true); }} />
          {shownName && !readOnly ? (
            <button type="button" className={styles.clear} aria-label={t("databaseView:clearValue")}
              onClick={() => { setDraftName(""); setNameCleared(true); }}>
              <XCircleIcon size="small" aria-hidden="true" />
            </button>
          ) : null}
        </label>
        {active !== null ? (
          <label className={styles.activeRow}>
            <SwitchInput checked={active} disabled={readOnly || busy || !onToggleActive} data-phone-automation-active
              onChange={() => onToggleActive?.()} />
            <span>{t("databaseView:automationActive")}</span>
          </label>
        ) : null}
        <p className={styles.scope} data-phone-automation-scope-row>
          {draftTrigger.type === "schedule" ? <span className={styles.scopeName}>{t("databaseView:automationScopeDatabase", { name: databaseName })}</span> : <>
            {scopeBefore}
            <button type="button" className={styles.scopeButton} disabled={readOnly} onClick={() => setPicking({ kind: "scope" })}
              data-phone-automation-scope-pick>
              <span className={styles.scopeName}>{scopeName}</span><ChevronDown size="tiny" aria-hidden="true" />
            </button>
            {scopeAfter}
          </>}
        </p>
        <section className={styles.section} aria-label={t("databaseView:automationTrigger")}>
          {head}
          {draftTrigger.type === "schedule"
            ? card("schedule", <RepeatIcon size="small" />, (
              <span className={styles.scheduleTitle}>{chipAround.before ? <span>{chipAround.before}</span> : null}
                <span className={styles.scheduleChip} data-phone-schedule-open>
                  {scheduleChipLabel(t, draftTrigger)}<ChevronDown size="tiny" aria-hidden="true" />
                </span>
                {chipAround.after ? <span>{chipAround.after}</span> : null}
              </span>
            ), () => setPicking({ kind: draftTrigger.pending ? "presets" : "schedule" }))
            : conditions.map((condition) => condition.type === "row_added"
              ? card(condition.id, <Plus size="small" />, t("databaseView:automationSummary.pageAdded"))
              : !condition.propertyId ? card(condition.id, <Pencil size="small" />, t("databaseView:automationSummary.anyPropertyEdited"))
              : (() => {
                const property = properties.find(({ id }) => id === condition.propertyId);
                if (!property) {
                  return card(condition.id, <GonePropertyMark propertyId={condition.propertyId} fallback={<LightningIcon size="small" />} />,
                    <GonePropertyWords propertyId={condition.propertyId} />,
                    (element) => setPicking({ kind: "gone", id: condition.id, propertyId: condition.propertyId,
                      anchor: element.closest<HTMLElement>("[data-phone-automation-condition]") ?? element }), true);
                }
                return card(condition.id, <PropertyIcon property={property} size="small" />,
                  <TriggerConditionSentence property={property} condition={{ operator: condition.operator, value: condition.value }} />,
                  triggerConditionKind(property) !== "none" ? () => setPicking({ kind: "condition", id: condition.id, propertyId: property.id,
                    draft: { operator: condition.operator, value: condition.value } }) : undefined);
              })())}
          {!readOnly && (draftTrigger.type === "events" && (canAdd || !conditions.length)) ? (
            <button type="button" className={styles.add} onClick={() => setPicking({ kind: "add" })} data-phone-automation-add-condition>
              <Plus size="regular" aria-hidden="true" />{t(conditions.length ? "databaseView:automationAddCondition" : "databaseView:automationNewCondition")}
            </button>
          ) : null}
        </section>
        <span className={styles.line} aria-hidden="true" />
        <section className={styles.section} aria-label={t("databaseView:automationAction")}>
          <p className={styles.sectionLabel}>{t("databaseView:automationAction")}</p>
          <fieldset className={styles.actions} disabled={readOnly}>
            <AutomationPhoneSheets.Provider value={true}>
              <AutomationActionEditor surface={draftSurface} bounds={databaseAutomationEditorBounds} actions={draftActions}
                properties={properties} pages={editorPages} views={views} editsItsPage={editsItsPage} warnEarlierOnAdd onChange={setDraftActions} />
            </AutomationPhoneSheets.Provider>
          </fieldset>
        </section>
      </div>
      {picking?.kind === "add" ? (
        <PhoneSheetMenu label={t("databaseView:automationFindCondition")} onClose={close}>
          <PhoneSheetSearch value={query} autoFocus={fieldFocusOnOpen()} placeholder={t("databaseView:automationFindCondition")}
            aria-label={t("databaseView:automationFindCondition")} onValueChange={setQuery} />
          {choices.map((choice) => (
            <PhoneSheetRow key={choice.key} icon={choiceMark(choice.icon, choice.key)} label={choice.label} more={choice.more} onPress={() => void pick(choice.key)}
              data={{ "data-phone-automation-choice": choice.key }} />
          ))}
        </PhoneSheetMenu>
      ) : null}
      {picking?.kind === "gone" ? (
        <AutomationPhoneSheets.Provider value={true}>
          <GonePropertyMenu anchor={picking.anchor} opener={null} propertyId={picking.propertyId}
            deleteLabel={t("databaseView:automationDeleteCondition")} onClose={close} onDelete={() => removeCondition(picking.id)} />
        </AutomationPhoneSheets.Provider>
      ) : null}
      {picking?.kind === "condition" ? (() => {
        // The screen edits its condition as it goes: "←" keeps a card's as "완료" does, a new one's goes back to the list
        // unmade, as Notion's (seen 2026-11-17 on a Galaxy A90 at 411; its desktop window keeps it on Escape and a press outside).
        const property = properties.find(({ id }) => id === picking.propertyId);
        return property ? <PhoneTriggerConditionScreen property={property} condition={picking.draft}
          onChange={(draft) => setPicking({ ...picking, draft })}
          onBack={() => picking.id ? saveCondition(picking) : setPicking({ kind: "add" })} onDone={() => saveCondition(picking)} /> : null;
      })() : null}
      {picking?.kind === "menu" ? (
        <PhoneSheetMenu label={t("databaseView:automationConditionMenu")} onClose={close}>
          {/* A condition's "삭제" is dark, as Notion's; an automation's and an action's red. */}
          <PhoneSheetRow icon={<Trash size="regular" />} label={t("common:actions.delete")} more={false}
            data={{ "data-phone-automation-condition-delete": "" }} onPress={() => { removeCondition(picking.id); close(); }} />
        </PhoneSheetMenu>
      ) : null}
      {picking?.kind === "mode" ? (
        <PhoneChoiceSheet title="" onClose={close}
          choices={[{ key: "all" as const, label: t("databaseView:automationModeAll") }, { key: "any" as const, label: t("databaseView:automationModeAny") }]}
          onPick={(mode) => setDraftTrigger((current) => current.type === "events" ? { ...current, mode } : current)} />
      ) : null}
      {picking?.kind === "scope" ? (
        <PhoneAutomationScopeScreen databaseName={databaseName} databaseIcon={databaseIcon} views={views} scope={draftScope} onPick={setDraftScope} onClose={close} />
      ) : null}
      {picking?.kind === "presets" && draftTrigger.type === "schedule" ? (
        <PhoneSchedulePresets onClose={close} onPick={(preset) => {
          setDraftTrigger(withSchedulePreset(draftTrigger, preset));
          setPicking({ kind: "schedule" });
        }} />
      ) : null}
      {picking?.kind === "schedule" && draftTrigger.type === "schedule" ? (
        <PhoneScheduleSheet draft={draftTrigger} readOnly={readOnly} onChange={setDraftTrigger} onClose={close} />
      ) : null}
    </PhoneSheet>
  );
}
