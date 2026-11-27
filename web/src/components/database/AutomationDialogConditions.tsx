import { useEffect, useRef, useState, type Dispatch, type ReactNode, type SetStateAction } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, DotsHorizontal, LightningIcon, Pencil, Plus, RepeatIcon, Trash } from "@/icons/leafen";
import type { DbProperty } from "@/lib/types";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { useHoverTip } from "@/components/IconTipButton";
import { automationCardWords } from "@/components/automation/AutomationActionCard";
import { GonePropertyMark, GonePropertyMenu, GonePropertyWords } from "@/components/automation/AutomationGoneProperty";
import { AutomationChoicePopover, AutomationPropertyMark, type AutomationChoice } from "@/components/automation/AutomationChoicePopover";
import { PropertyIcon } from "./PropertyIcon";
import { AutomationScheduleDetails } from "./AutomationScheduleDetails";
import { TriggerConditionSentence, type TriggerConditionValue } from "./PhoneAutomationCondition";
import { AutomationConditionValuePopover } from "./AutomationConditionValuePopover";
import { defaultTriggerCondition, settledTriggerCondition, triggerConditionKind } from "./automationTriggerConditions";
import { SCHEDULE_PRESETS, scheduleChipAround, scheduleChipLabel, withSchedulePreset } from "./automationScheduleModel";
import { hasTriggerCondition, modeHeadWords, triggerConditions, triggerNamesProperties, triggerSaysMode, withAnyPropertyEdited,
  withConditionValue, withoutTriggerCondition, withTriggerCondition } from "./automationConditionEditing";
import { isDatabaseAutomationListedProperty, type AutomationTriggerDraft } from "./databaseAutomationPanelModel";
import styles from "./DatabaseAutomationDialog.module.css";
import chipStyles from "./AutomationModeChip.module.css";

type Open = { kind: "add" } | { kind: "menu"; id: string } | { kind: "mode" } | { kind: "presets" } | { kind: "details" }
  /** A property's value, picked where "+ 새 조건" listed it (with "←") or under its card. */
  | { kind: "condition"; id?: string; propertyId: string; draft: TriggerConditionValue }
  /** A deleted property's card pressed (`opener`, its button): "속성 복원" and "조건 삭제". */
  | { kind: "gone"; id: string; propertyId: string; opener: HTMLElement }
  | null;

const cardWords = (card: HTMLElement) => automationCardWords(card.querySelector("[data-card-label]"));

/**
 * The desktop dialog's "다음 경우에 사용:", as Notion's (seen 2026-10-24 at
 * 1280px): each condition a white card of its mark and words 12px in, 44px
 * (46.4 with value chips), its "⋯" and its words' tip showing as the pointer
 * is on it (seen 2026-11-10, 2026-11-11),
 * "+ 새 조건" (then "+ 조건 추가") a ringed 46px row opening a menu of
 * "이벤트" — any property edited, a page added, "매..." while there is
 * nothing else — and "속성", "›" after one asking what its edit leaves.
 * "매..." asks its "주기" at once, and its details open under the chip as it
 * is picked; the chip opens them again (AutomationScheduleDetails, seen
 * 2026-10-25, 2026-11-08).
 * A property that can say what its edit leaves asks it in a window of its
 * own (AutomationConditionValuePopover), and its card opens that again. While
 * a condition names a property, "모든" or "하나라도" in a chip says how those
 * join (triggerSaysMode).
 * A condition on a deleted property is a red card, its tip saying to restore
 * or delete it, pressed for "속성 복원" and "조건 삭제" (AutomationGoneProperty).
 */
export function AutomationDialogConditions({ draftTrigger, setDraftTrigger, properties, offered = properties, triggerPropertyLabels, readOnly, selectSchedule }: {
  draftTrigger: AutomationTriggerDraft;
  setDraftTrigger: Dispatch<SetStateAction<AutomationTriggerDraft>>;
  properties: DbProperty[];
  /** The properties "+ 새 조건" offers: People's four, else all it lists. */
  offered?: DbProperty[];
  triggerPropertyLabels: string[];
  readOnly: boolean;
  /** "매...": the trigger becomes a schedule still to be picked. */
  selectSchedule(): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const around = scheduleChipAround(t);
  const [open, setOpen] = useState<Open>(null);
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const [presetsNext, setPresetsNext] = useState(false);
  const chipRef = useRef<HTMLButtonElement>(null);
  const cardTip = useHoverTip();
  const goneTip = t("databaseView:automationGonePropertyTip");
  const conditions = triggerConditions(draftTrigger);
  const modeKey = draftTrigger.type === "events" && draftTrigger.mode === "all" ? "databaseView:automationModeAll" : "databaseView:automationModeAny";
  const [modeBefore, modeAfter] = modeHeadWords(t);
  const labelOf =(propertyId: string) => triggerPropertyLabels[properties.findIndex(({ id }) => id === propertyId)] ?? t("databaseView:untitled");
  const openFrom = (next: Open, element: HTMLElement | null) => { setAnchor(element); setOpen(next); };
  const close = () => setOpen(null);
  // "매..." opens its "주기" once its card is drawn.
  useEffect(() => {
    if (!presetsNext || !chipRef.current) return;
    setPresetsNext(false);
    openFrom({ kind: "presets" }, chipRef.current);
  }, [presetsNext, draftTrigger]);
  const events = draftTrigger.type === "events";
  const group = { events: t("databaseView:automationEventsGroup"), properties: t("databaseView:automationPropertiesGroup") };
  const addChoices: AutomationChoice[] = [
    ...(events && !hasTriggerCondition(draftTrigger, { type: "property_edited", propertyId: "" })
      ? [{ key: "any_edit", icon: <Pencil size="regular" />, text: t("databaseView:automationAnyPropertyEdited"), group: group.events }] : []),
    ...(!hasTriggerCondition(draftTrigger, { type: "row_added" })
      ? [{ key: "row_added", icon: <Plus size="regular" />, text: t("databaseView:automationPageAddedDone"), group: group.events }] : []),
    ...(!conditions.length && events ? [{ key: "schedule", icon: <RepeatIcon size="regular" />, text: t("databaseView:automationEvery"), group: group.events }] : []),
    ...offered.filter((property) => isDatabaseAutomationListedProperty(property)
      && !hasTriggerCondition(draftTrigger, { type: "property_edited", propertyId: property.id }))
      .map((property) => ({ key: `property:${property.id}`, icon: <AutomationPropertyMark property={property} />, text: labelOf(property.id),
        group: group.properties, more: triggerConditionKind(property) !== "none" })),
  ];
  const pick = async (key: string) => {
    if (key === "schedule") {
      selectSchedule();
      setPresetsNext(true);
    } else if (key === "row_added") {
      setDraftTrigger((current) => withTriggerCondition(current, { type: "row_added" }));
    } else if (key === "any_edit") {
      // Any property's edit takes the place of each property's, once asked (Notion's, seen 2026-10-24).
      if (triggerNamesProperties(draftTrigger) && !await confirmAction({ message: t("databaseView:automationReplaceTitle"),
        detail: t("databaseView:automationReplaceDetail"), confirmLabel: t("databaseView:automationReplace"), danger: true })) return;
      setDraftTrigger((current) => withAnyPropertyEdited(current));
    } else {
      const property = properties.find(({ id }) => id === key.slice("property:".length));
      // A property that can say what its edit leaves asks it first, where the menu was.
      if (property && triggerConditionKind(property) !== "none") {
        setOpen({ kind: "condition", propertyId: property.id, draft: defaultTriggerCondition(property) });
        return;
      }
      setDraftTrigger((current) => withTriggerCondition(current, { type: "property_edited", propertyId: key.slice("property:".length) }));
    }
  };
  const canAdd = events && (!hasTriggerCondition(draftTrigger, { type: "row_added" })
    || offered.some((property) => !hasTriggerCondition(draftTrigger, { type: "property_edited", propertyId: property.id })));
  // A property's sentence sets its own weights and lines (TriggerConditionSentence); a deleted property's words are 400.
  // A deleted property's card tips what to do instead of its words (Notion's, seen 2026-11-11).
  // A card deleted from its "⋯" leaves the keys on the page, nothing ringed (Notion's, seen 2026-11-15 in two tabs).
  const card = (key: string, icon: ReactNode, label: ReactNode, onPress?: (element: HTMLElement) => void, gone = false,
    sentence = false) => (
    <div key={key} className={styles.card} data-automation-condition={key} data-gone={gone || undefined} data-focus-leaves
      onMouseEnter={(event) => cardTip.enter(event.currentTarget, gone ? goneTip : cardWords(event.currentTarget))}
      onMouseLeave={cardTip.leave} onPointerDown={cardTip.leave}>
      <span className={styles.cardIcon} aria-hidden="true">{icon}</span>
      {onPress && !readOnly
        ? <button type="button" className={styles.cardLabel} data-card-label data-sentence={sentence || undefined} data-gone={gone || undefined}
          onClick={(event) => onPress(event.currentTarget)}>{label}</button>
        : <span className={styles.cardLabel} data-card-label data-sentence={sentence || undefined} data-gone={gone || undefined}>{label}</span>}
      {readOnly ? null : (
        // Its tip hides while the pointer is on "⋯" and comes back off it (Notion's, seen 2026-11-11).
        <span className={styles.cardTools} onMouseEnter={cardTip.leave} onMouseLeave={(event) => {
          const at = event.currentTarget.closest<HTMLElement>("[data-automation-condition]");
          if (at) cardTip.enter(at, at.dataset.gone ? goneTip : cardWords(at));
        }}>
          <button type="button" className={styles.cardDots} aria-label={t("databaseView:automationConditionMenu")} aria-haspopup="menu"
            onClick={(event) => openFrom({ kind: "menu", id: key }, event.currentTarget)}>
            <DotsHorizontal size="regular" aria-hidden="true" />
          </button>
        </span>
      )}
    </div>
  );
  return (
    <section className={styles.section} aria-label={t("databaseView:automationTrigger")} data-conditions>
      {triggerSaysMode(draftTrigger) ? (
        <p className={styles.sectionLabel}>
          {modeBefore}
          <button type="button" className={chipStyles.modeChip} disabled={readOnly} data-automation-mode
            onClick={(event) => openFrom({ kind: "mode" }, event.currentTarget)}>
            {t(modeKey)}
            <ChevronDown size="tiny" trim glyphWidth={9.88} aria-hidden="true" />
          </button>
          {modeAfter}
        </p>
      ) : <p className={styles.sectionLabel}>{t("databaseView:automationUseWhen")}</p>}
      {draftTrigger.type === "schedule" ? (
        <>
          <div className={styles.card} data-automation-condition="schedule">
            <span className={styles.cardIcon} aria-hidden="true"><RepeatIcon size="small" /></span>
            {/* "Every" before the chip in English, none in Korean (Notion's, seen 2026-11-19). */}
            <span className={styles.scheduleTitle}>{around.before ? <span>{around.before}</span> : null}
              <button ref={chipRef} type="button" className={styles.scheduleChip} disabled={readOnly} data-automation-schedule-chip
                aria-haspopup="dialog" aria-expanded={open?.kind === "details"}
                onClick={(event) => openFrom({ kind: draftTrigger.pending ? "presets" : "details" }, event.currentTarget)}>
                {scheduleChipLabel(t, draftTrigger)}<ChevronDown size="tiny" aria-hidden="true" />
              </button>
              {around.after ? <span>{around.after}</span> : null}
            </span>
            {readOnly ? null : (
              <span className={styles.cardTools}>
                <button type="button" className={styles.cardDots} aria-label={t("databaseView:automationConditionMenu")} aria-haspopup="menu"
                  onClick={(event) => openFrom({ kind: "menu", id: "schedule" }, event.currentTarget)}>
                  <DotsHorizontal size="regular" aria-hidden="true" />
                </button>
              </span>
            )}
          </div>
        </>
      ) : conditions.map((condition) => condition.type === "row_added"
        ? card(condition.id, <Plus size="small" />, t("databaseView:automationSummary.pageAdded"))
        : !condition.propertyId ? card(condition.id, <Pencil size="small" />, t("databaseView:automationSummary.anyPropertyEdited"))
        : (() => {
          const property = properties.find(({ id }) => id === condition.propertyId);
          const asks = property && triggerConditionKind(property) !== "none";
          // The value's window hangs from the card's edge, as Notion's; a property with no value to ask ("날짜 속성 편집")
          // is words, not a button (seen 2026-11-10). A deleted property's is Notion's red card, which opens "속성 복원"
          // and "조건 삭제" under it (seen 2026-11-11).
          if (!property) {
            return card(condition.id, <GonePropertyMark propertyId={condition.propertyId} fallback={<LightningIcon size="small" />} />,
              <GonePropertyWords propertyId={condition.propertyId} />,
              (element) => openFrom({ kind: "gone", id: condition.id, propertyId: condition.propertyId, opener: element },
                element.closest<HTMLElement>("[data-automation-condition]") ?? element), true, true);
          }
          return card(condition.id, <span className={styles.propertyMark}><PropertyIcon property={property} size="small" /></span>,
            <TriggerConditionSentence property={property} condition={{ operator: condition.operator, value: condition.value }} />,
            asks ? (element) => openFrom({ kind: "condition", id: condition.id, propertyId: condition.propertyId,
              draft: { operator: condition.operator, value: condition.value } }, element.closest<HTMLElement>("[data-automation-condition]") ?? element)
              : undefined, false, true);
        })())}
      {cardTip.tip}
      {!readOnly && events && (canAdd || !conditions.length) ? (
        <button type="button" className={styles.add} data-automation-add-condition
          onClick={(event) => openFrom({ kind: "add" }, event.currentTarget)}>
          <Plus size="small" aria-hidden="true" />{t(conditions.length ? "databaseView:automationAddCondition" : "databaseView:automationNewCondition")}
        </button>
      ) : null}
      {open?.kind === "add" ? (
        <AutomationChoicePopover label={t("databaseView:automationFindCondition")} searchPlaceholder={t("databaseView:automationFindCondition")} flush greyMarks
          trigger={anchor} onClose={close} onPick={(key) => void pick(key)}
          choices={addChoices} />
      ) : null}
      {open?.kind === "condition" ? (() => {
        const property = properties.find(({ id }) => id === open.propertyId);
        // The window edits its condition as it goes: "완료", Escape and a press outside all keep it (a new one made then),
        // "←" goes back to the list with a new one unmade, as Notion's (seen 2026-11-17 in Chrome at 1440 in two tabs).
        const keep = () => { setDraftTrigger((current) => withConditionValue(current, open.id, open.propertyId, settledTriggerCondition(property, open.draft))); close(); };
        return property ? (
          <AutomationConditionValuePopover property={property} condition={open.draft} trigger={anchor} onClose={keep}
            onBack={open.id ? undefined : () => setOpen({ kind: "add" })}
            onChange={(draft) => setOpen({ ...open, draft })} onDone={keep} />
        ) : null;
      })() : null}
      {open?.kind === "menu" ? (
        // 180 wide, centred right under "⋯", as Notion's (seen 2026-11-15 at 1440 in two tabs: its middle on the dots', its top on their foot).
        <AutomationChoicePopover label={t("databaseView:automationConditionMenu")} trigger={anchor} width={180} align="center" flush onClose={close} pickDropsFocus afterEmptyGroup
          choices={[{ key: "delete", icon: <Trash size="regular" />, text: t("common:actions.delete") }]}
          onPick={() => setDraftTrigger((current) => withoutTriggerCondition(current, open.id))} />
      ) : null}
      {open?.kind === "gone" && anchor ? (
        <GonePropertyMenu anchor={anchor} opener={open.opener} propertyId={open.propertyId} deleteLabel={t("databaseView:automationDeleteCondition")} onClose={close}
          onDelete={() => setDraftTrigger((current) => withoutTriggerCondition(current, open.id))} />
      ) : null}
      {open?.kind === "mode" ? (
        <AutomationChoicePopover label={t("databaseView:automationModeHead", { mode: t(modeKey) })} trigger={anchor} width={180} onClose={close}
          choices={(["all", "any"] as const).map((mode) => ({ key: mode, text: t(mode === "all" ? "databaseView:automationModeAll" : "databaseView:automationModeAny"),
            chosen: draftTrigger.type === "events" && draftTrigger.mode === mode }))}
          onPick={(mode) => setDraftTrigger((current) => current.type === "events" ? { ...current, mode: mode === "all" ? "all" : "any" } : current)} />
      ) : null}
      {open?.kind === "details" && draftTrigger.type === "schedule" && !draftTrigger.pending ? (
        // Only what changes marks it changed: a field left as it was saves nothing, as Notion's.
        <AutomationScheduleDetails draft={draftTrigger} readOnly={readOnly} chipRef={chipRef} onClose={close} onChange={setDraftTrigger} />
      ) : null}
      {open?.kind === "presets" && draftTrigger.type === "schedule" ? (
        <AutomationChoicePopover label={t("databaseView:schedulePending")} trigger={anchor} width={220} onClose={close}
          choices={SCHEDULE_PRESETS.map((preset) => ({ key: preset, text: t(`databaseView:schedulePresets.${preset}`) }))}
          onPick={(preset) => {
            setDraftTrigger((current) => current.type === "schedule"
              ? { ...withSchedulePreset(current, preset as typeof SCHEDULE_PRESETS[number]), pending: false } : current);
            // Its details open at once under the chip, as Notion's (seen 2026-11-08).
            openFrom({ kind: "details" }, chipRef.current);
          }} />
      ) : null}
    </section>
  );
}
