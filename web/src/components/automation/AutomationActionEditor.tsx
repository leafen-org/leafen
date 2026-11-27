"use client";

import { useContext, useEffect, useId, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, SlidersIcon } from "@/icons/leafen";
import { disambiguatePropertyLabels } from "@/components/database/propertyOptionLabels";
import type { AutomationAction, DbProperty, DbView, EditPropertyAutomationAction, Page } from "@/lib/types";
import { useProductPages } from "@/components/product/productPagesContext";
import { useUsableAppList } from "@/components/apps/useUsableApps";
import { AutomationActionFields } from "./AutomationActionFields";
import { AutomationSlackChannelChip } from "./AutomationSlackChannelChip";
import { automationFormulaNames, earlierAutomationVariables } from "./automationFormulaNames";
import { automationActionFormulaFault, automationCheckedNames } from "./automationFormulaFault";
import {
  MAX_WEBHOOK_ACTIONS,
  TRIGGER_PAGE_TYPES,
  automationActionProblem,
  automationActionTypesForSurface,
  automationPropertyOrder,
  databasePages,
  defaultPropertyValue,
  labelKey,
  menuLabelKey,
  newAutomationEditorAction,
  type AutomationActionType,
  type AutomationEditorSurface,
} from "./automationActionModel";
import { isDatabaseAutomationEditableProperty, isDatabaseAutomationListedProperty } from "@/components/database/databaseAutomationPanelModel";
import { AutomationEditPropertyEditor, AutomationPropertyPick, AutomationValueFormula, AutomationValuePopover, EditPropertySentence, OPTION_TYPES } from "./AutomationEditProperty";
import {
  AutomationRecipientKindChip,
  automationRecipientKindOf,
  automationRecipientsOfKind,
} from "./AutomationRecipientKind";
import editStyles from "./AutomationEditProperty.module.css";
import {
  admitAutomationEditorInputs,
  type AutomationEditorBounds,
} from "./automationActionBudget";
import type { NativeEditorGraphAdapter } from "@/components/editor/editorRuntimeTypes";
import { editorContentEqual } from "@/components/editor/editorContentDelta";
import { newId } from "@/lib/ids";
import { AutomationInsertionPosition } from "./AutomationInsertionPosition";
import { AutomationActionCard } from "./AutomationActionCard";
import { AutomationActionIcon, automationMarkTip } from "./AutomationActionIcon";
import { EditPropertyCardMark } from "./AutomationEditPropertyMark";
import { automationVariableNamesOf, freshAutomationVariableName } from "./automationVariableNames";
import { AutomationAddPageTemplate } from "./AutomationAddPageFields";
import { aimEditPagesAtThisPage, AutomationEditPagesScope, retargetEditPages, type PagesAutomationAction } from "./AutomationEditPagesFields";
import { AutomationTargetDatabasePicker } from "./AutomationTargetDatabasePicker";
import { AutomationOpenTargetPicker } from "./AutomationOpenTargetPicker";
import { AutomationOpenInChip } from "./AutomationOpenInChip";
import { AutomationActionTypeMenu, AutomationPhoneSheets, actionTypeMenuRoom, floatingAbove, type AutomationActionTypePage } from "./AutomationActionMenus";
import { useAutomationTargetDatabases } from "./automationTargetDatabases";
import { GonePropertyGlyph, GonePropertyMenu } from "./AutomationGoneProperty";
import styles from "./automationActionEditor.module.css";

export * from "./automationActionModel";

// Where a heading's chip sits in its sentence, each language's word order
// being its own ("{{target}}에 페이지 추가", "Add page to {{target}}").
const CHIP = "\u0000";
const headingAround = (sentence: string) => {
  const [before = "", after = ""] = sentence.split(CHIP);
  return { before: before.trim(), after: after.trim() };
};
export type { AutomationEditorBounds } from "./automationActionBudget";

export function AutomationActionEditor({
  surface,
  bounds,
  actions,
  properties = [],
  pages = [],
  views = [],
  editsItsPage = true,
  draftContext,
  draftOwnerId,
  readActions,
  newWhenEmpty = false,
  warnEarlierOnAdd = false,
  revealProblems = 0,
  onWarnings,
  onChange,
}: {
  surface: AutomationEditorSurface;
  bounds: AutomationEditorBounds;
  actions: AutomationAction[];
  properties?: DbProperty[];
  pages?: Page[];
  views?: DbView[];
  /** Whether a step may edit the page it ran for ("속성 편집…"): People's automations may not. */
  editsItsPage?: boolean;
  draftContext?: NativeEditorGraphAdapter;
  draftOwnerId?: string;
  readActions?: () => AutomationAction[];
  /** An automation's: "+ 새 작업" until it has one, as Notion's dialog and sheet. */
  newWhenEmpty?: boolean;
  /**
   * A database automation's: adding an action warns on every one before it
   * left unfinished, as Notion's (seen 2026-11-02; its buttons' editor
   * doesn't), the one just added still gray.
   */
  warnEarlierOnAdd?: boolean;
  /**
   * Counts presses of a dimmed "활성화": each marks every action as asked
   * for, its mark and what it lacks in red, as Notion's (seen 2026-11-08).
   */
  revealProblems?: number;
  /** Whether any action shows its unfinished warning, for the window's foot. */
  onWarnings?: (shown: boolean) => void;
  onChange: (actions: AutomationAction[]) => void;
}) {
  const { t } = useTranslation(["databaseView"]);
  const variableName = (number: number) => t("databaseView:variableDefaultName", { number });
  const phone = useContext(AutomationPhoneSheets);
  admitAutomationEditorInputs(actions, properties, pages, views, bounds);
  const [menuOpen, setMenuOpen] = useState(false);
  const [menuStart, setMenuStart] = useState<AutomationActionType | undefined>();
  // An edit-property action being set: just added (its "←" back to the properties), or opened from its card,
  // a formula's in its formula editor alone.
  const [editing, setEditing] = useState<{ id: string; fresh: boolean; picking: boolean; formula?: boolean } | null>(null);
  // On a desktop a property just chosen asks its value in a window under where it was asked for, and its
  // card comes once a value is: until then the action is only this draft (Notion's, seen 2026-11-02).
  const [pending, setPending] = useState<{ action: EditPropertyAutomationAction; beforeId?: string; anchor: DOMRect; added: boolean; above?: boolean } | null>(null);
  // The draft whose card a value made, before it shows as added.
  const pendingAdded = useRef<string | null>(null);
  // Where an open card's value window hangs: under the card.
  const [cardAnchor, setCardAnchor] = useState<DOMRect | null>(null);
  // A deleted property's card pressed: "속성 복원" and "작업 삭제" under it (Notion's, seen 2026-11-11).
  const [goneMenu, setGoneMenu] = useState<{ id: string; propertyId: string; anchor: HTMLElement; opener: HTMLElement | null } | null>(null);
  const addRef = useRef<HTMLDivElement>(null);
  const latest = useRef({ actions, onChange, readActions }); latest.current = { actions, onChange, readActions };
  const currentActions = () => latest.current.readActions?.() ?? latest.current.actions;
  const copying = useRef(new Set<string>());
  // An unfinished choice warns only on actions saved before this editor
  // opened, as Notion's: one just added starts gray (seen 2026-10-10).
  const saved = useRef<Set<string> | null>(null);
  const savedIds = saved.current ??= new Set(actions.map((action) => action.id));
  const warnEarlier = (actions: readonly AutomationAction[]) => {
    if (warnEarlierOnAdd) for (const action of actions) savedIds.add(action.id);
  };
  const [, setRevealed] = useState(0);
  useEffect(() => {
    if (!revealProblems) return;
    for (const action of latest.current.actions) savedIds.add(action.id);
    setRevealed(revealProblems);
  }, [revealProblems, savedIds]);
  const databases = useMemo(() => databasePages(pages), [pages]);
  // The database the actions belong to, decided here alone: a database's automation's or button's own, the one its
  // properties are in — not the first of the page's databases, which leads its lists — and none for a page's button,
  // though its page holds databases. Its "↗ 이 페이지" and own forms are this one's (Notion's open list, seen
  // 2026-11-24: a database's button on its second source offers "이 페이지", a page's button under a database none).
  const ownDatabaseId = surface === "page_button" ? undefined : properties[0]?.databaseId;
  const ownDatabase = useMemo(() => databases.find(({ id }) => id === ownDatabaseId), [databases, ownDatabaseId]);
  const targets = useAutomationTargetDatabases(actions, databases, draftContext?.workspaceId);
  // What a formula is checked against: this database's properties, and those of a database a page is added to or edited in.
  const checked = useMemo(() => targets.properties?.length ? [...properties, ...targets.properties] : properties, [properties, targets.properties]);
  // The names a step's formulas run with: those of where it runs and the variables the steps before it define.
  const namesAt = (index: number) => automationCheckedNames(surface, earlierAutomationVariables(actions, index), properties);
  const problemOf = (action: AutomationAction) => automationActionProblem(action, checked, namesAt(actions.findIndex(({ id }) => id === action.id)));
  // A formula of a type its property can't take warns at once, as Notion's (seen 2026-11-10): "속성 편집"'s or a page
  // line's; so does "속성 편집"'s that can't be read (seen 2026-11-21).
  const shownProblem = (action: AutomationAction) => {
    const problem = problemOf(action);
    return problem !== null && (savedIds.has(action.id) || problem.endsWith(".formulaType") || problem.endsWith(".formulaParse")) ? problem : null;
  };
  const warned = actions.some((action) => shownProblem(action) !== null);
  useEffect(() => { onWarnings?.(warned); }, [onWarnings, warned]);
  // A card's mark is red while it says what it lacks, once asked for: a page button's new notification too stays grey
  // (Notion's, seen 2026-11-21 in two tabs; red only in a save's blink about 0.9s in, which isn't followed).
  const markWarned = (action: AutomationAction) => shownProblem(action) !== null;
  // An automation or a button notifies once and sets its page's icon once: with one, the list — "+ 작업 추가" and a
  // card's "위에/아래에 작업 추가" — offers no other and its card no "복제" (Notion's, seen 2026-11-08 in a database's
  // automation and a page's button; the icon 2026-11-21 twice in a database's button, once in its automation).
  const once = new Set<AutomationActionType>(actions.map((action) => action.type).filter((type) => type === "send_notification" || type === "set_page_icon"));
  // A database's automation posts to Slack once too (Notion's, seen 2026-11-15); a page's button may post twice.
  const slacks = surface === "event_automation" && actions.some((action) => action.type === "send_slack");
  // A data source chosen and gone since says a data source is to be chosen, as a red card's head does (seen 2026-11-09).
  const goneTip = t("databaseView:actionChipProblems.destination");
  const owner = useId();
  // The organization's apps with actions an automation may run (Leafen).
  const product = useProductPages();
  const organizationId = product?.organizationId;
  const { apps: usable } = useUsableAppList(product?.session, Boolean(organizationId));
  const apps = useMemo(() => usable.filter((app) => app.tenancyKey === `org:${organizationId}` && app.automations?.length), [organizationId, usable]);
  const webhookCount = actions.filter((action) => action.type === "send_webhook").length;
  const allowedTypes = useMemo(() => [
    ...automationActionTypesForSurface(surface).filter((type) => (type !== "send_webhook" || webhookCount < MAX_WEBHOOK_ACTIONS)
      && (editsItsPage || !TRIGGER_PAGE_TYPES.has(type))),
    ...(apps.length ? ["run_app_action" as const] : []),
  ], [apps.length, editsItsPage, surface, webhookCount]);
  const propertyLabels = useMemo(
    () => disambiguatePropertyLabels(properties, t("databaseView:untitled")),
    [properties, t],
  );

  function replace(actionId: string, next: AutomationAction) {
    latest.current.onChange(currentActions().map((action) => action.id === actionId ? next : action));
  }

  function add(type: AutomationActionType, beforeId?: string) {
    const actions = currentActions();
    if (!allowedTypes.includes(type)) return;
    const insertion = beforeId ? actions.findIndex(action => action.id === beforeId) : actions.length;
    if (insertion < 0) return;
    const action = newAutomationEditorAction(type, { properties, pages, views, database: ownDatabase }, bounds);
    if (action.type === "open_page") action.pageId = "";
    // Its first variable is numbered after every one the automation has (freshAutomationVariableName).
    if (action.type === "define_variables") action.variables[0].name = freshAutomationVariableName(automationVariableNamesOf(actions), variableName);
    if (action.type === "run_app_action" && apps[0]) {
      action.appInstallId = apps[0].installId;
      action.appAction = apps[0].automations?.[0]?.key ?? "";
    }
    const next = [...actions]; next.splice(insertion, 0, action);
    warnEarlier(actions);
    onChange(next);
    setMenuOpen(false);
  }

  function move(sourceId: string, targetId: string, after: boolean) {
    const actions = currentActions();
    const source = actions.findIndex(action => action.id === sourceId);
    const target = targetId === "previous" ? source - 1 : targetId === "next" ? source + 1
      : actions.findIndex(action => action.id === targetId);
    if (source < 0 || target < 0 || target >= actions.length || source === target) return;
    const destination = actions[target].id;
    const next = actions.filter(action => action.id !== sourceId);
    next.splice(next.findIndex(action => action.id === destination) + (after ? 1 : 0), 0, actions[source]);
    onChange(next);
  }
  const editable = useMemo(() => properties.filter(isDatabaseAutomationListedProperty).sort(automationPropertyOrder), [properties]);
  const buttonRowDatabase = surface === "database_button" ? ownDatabaseId : undefined;
  // A property another action already sets is left out — "속성 편집..."'s, or a line of a page edit in this
  // database — and once every one is "속성 편집..." goes from the list too (Notion's, seen 2026-11-10).
  const unset = (except?: string) => editable.filter(property => !actions.some(action => action.id !== except
    && (action.type === "edit_property" ? action.propertyId === property.id
      : action.type === "edit_pages" && action.target.databaseId === property.databaseId
        && action.changes.some(change => change.propertyId === property.id))));
  const allSet = !buttonRowDatabase && editable.length > 0 && !unset().length;
  // Opening is one row, "페이지, 양식 또는 URL 열기", its card choosing which (Notion's, seen 2026-11-02).
  const types = allowedTypes.filter(type => type !== "open_form" && type !== "open_url" && !once.has(type)
    && !(slacks && type === "send_slack") && !(allSet && type === "edit_property")).map(type => ({ type, label: t(menuLabelKey(type)) }));
  // "속성 편집..." asks its property first, on the list's own page, then opens its value (Notion's, seen 2026-10-24).
  const propertyPage = (beforeId?: string): AutomationActionTypePage => (type, { close, back }) => type === "edit_property" && editable.length
    ? <AutomationPropertyPick properties={buttonRowDatabase ? editable : unset()} onBack={back} onClose={close}
      onPick={property => { close(); addProperty(property, beforeId); }} /> : undefined;

  // A button's edit or archive can aim at the row it is on, Notion's "↗ 이 페이지" (seen 2026-11-04); an automation's
  // archive at the page that set it off, "↗ 페이지 실행" (seen 2026-11-15), which a schedule has none of.
  const triggerDatabase = surface === "event_automation" && editsItsPage ? ownDatabaseId : undefined;
  const editThisPage = (action: PagesAutomationAction) => {
    const databaseId = buttonRowDatabase ?? (action.type === "archive_pages" ? triggerDatabase : undefined);
    return databaseId ? { chosen: action.target.type === "this_page", label: triggerDatabase ? t("databaseView:messageNames.triggerPage") : undefined,
      onPick: () => replace(action.id, aimEditPagesAtThisPage(action, databaseId)) } : undefined;
  };

  function addProperty(property: DbProperty, beforeId?: string) {
    const actions = currentActions();
    const insertion = beforeId ? actions.findIndex(action => action.id === beforeId) : actions.length;
    if (insertion < 0 || !allowedTypes.includes("edit_property")) return;
    // On a button "속성 편집..." makes the card for its own row, more properties to go in it (Notion's, seen 2026-11-02).
    if (buttonRowDatabase) {
      const card: AutomationAction = { id: newId(), type: "edit_pages", target: { type: "this_page", databaseId: buttonRowDatabase, filter: {} },
        changes: [{ propertyId: property.id, value: defaultPropertyValue(property) }] };
      setMenuOpen(false);
      setMenuStart(undefined);
      const next = [...actions]; next.splice(insertion, 0, card);
      onChange(next);
      return;
    }
    const made = newAutomationEditorAction("edit_property", { properties, pages, views, database: ownDatabase }, bounds);
    if (made.type !== "edit_property") return;
    const action = { ...made, propertyId: property.id, value: defaultPropertyValue(property) };
    setMenuOpen(false);
    setMenuStart(undefined);
    if (!phone) {
      const at = beforeId ? document.querySelector(`[data-automation-action="${CSS.escape(beforeId)}"]`) : addRef.current;
      // From "+ 작업 추가" it opens where the action list did (AutomationValuePopover).
      const anchor = at?.getBoundingClientRect();
      if (anchor) { setPending({ action, beforeId, anchor, added: false, above: !beforeId && floatingAbove(anchor, 0, actionTypeMenuRoom(types.length)) }); return; }
    }
    const next = [...actions]; next.splice(insertion, 0, action);
    onChange(next);
    setEditing({ id: action.id, fresh: true, picking: false });
  }
  // The draft's value: the first makes its card, where it was asked for; a
  // select's or a status's one choice also closes the window, as Notion's.
  // People and a multi-select's options are picked in the open window, their
  // card made as it closes and "←" still taking them back (Notion's, seen
  // 2026-11-11).
  function setPendingValue(next: AutomationAction, closing = false) {
    if (!pending || next.type !== "edit_property") return;
    const property = properties.find(candidate => candidate.id === next.propertyId);
    if ((property?.type === "person" || property?.type === "multi_select") && !pending.added && !closing) {
      setPending({ ...pending, action: next });
      return;
    }
    pendingAdded.current = next.id;
    const actions = currentActions();
    if (pending.added) replace(next.id, next);
    else {
      const insertion = pending.beforeId ? actions.findIndex(action => action.id === pending.beforeId) : actions.length;
      const list = [...actions]; list.splice(insertion < 0 ? actions.length : insertion, 0, next);
      warnEarlier(actions);
      latest.current.onChange(list);
    }
    setPending(property?.type === "select" || property?.type === "status" ? null : { ...pending, action: next, added: true });
  }
  // However the draft's window closes — "완료", Escape or a press outside — with nothing chosen it makes its card,
  // "메모 지우기" (Notion's, seen 2026-11-10: Escape on a text and a select, a press outside on a URL, "완료" on a
  // number and a date); only "←" takes it back.
  function finishPending() {
    if (pending && !pending.added && pendingAdded.current !== pending.action.id) setPendingValue(pending.action, true);
    setPending(null);
  }
  async function copy(action: AutomationAction) {
    if (copying.current.has(action.id)) return;
    copying.current.add(action.id);
    const cloned = { ...structuredClone(action), id: newId() };
    try {
      if (cloned.type === "insert_blocks" && cloned.sourceBlockId && draftContext) {
        const roots = await draftContext.copyOnServer([cloned.sourceBlockId]);
        if (roots.length !== 1 || !draftContext.isCurrent()) return;
        cloned.sourceBlockId = roots[0];
      }
      const current = { ...latest.current, actions: currentActions() };
      const index = current.actions.findIndex(candidate => candidate.id === action.id);
      if (index < 0 || !editorContentEqual(current.actions[index], action)) {
        if (cloned.type === "insert_blocks" && cloned.sourceBlockId !== (action.type === "insert_blocks" ? action.sourceBlockId : undefined)
          && draftContext?.isCurrent()) draftContext.remove([cloned.sourceBlockId!]);
        return;
      }
      const next = [...current.actions]; next.splice(index + 1, 0, cloned); current.onChange(next);
    } catch {
      draftContext?.moveContext?.onStatus?.(t("databaseView:automationCopyFailed"), "error");
    } finally {
      copying.current.delete(action.id);
    }
  }
  // An open card's value: in its window under the card on a desktop, in the card's own sheet on a phone.
  // An option list's window is 260 wide, another value's 266 (AutomationValuePopover).
  const wideFor = (propertyId: string | undefined) => !OPTION_TYPES.has(properties.find(property => property.id === propertyId)?.type ?? "");
  const inCardWindow = (content: React.ReactNode, wide: boolean) => !phone && cardAnchor
    ? <AutomationValuePopover anchor={cardAnchor} wide={wide} onClose={() => setEditing(null)}>{content}</AutomationValuePopover> : content;
  return <div className={styles.root}>
    <div className={styles.list}>
      {actions.map((action, index) => {
        const remove = () => latest.current.onChange(currentActions().filter(candidate => candidate.id !== action.id));
        // What a red card lacks: its head tips it, a red chip in it too (AutomationActionCard's problemTip).
        const markTip = automationMarkTip(t, problemOf(action));
        const edited = action.type === "edit_property" ? properties.find(property => property.id === action.propertyId) : undefined;
        // One setting a property the database deleted is Notion's red card (seen 2026-11-11): its type's mark on red.
        const gone = action.type === "edit_property" && action.propertyId && !edited ? action.propertyId : null;
        const open = action.type === "edit_property" && editing?.id === action.id ? editing : null;
        // A page's button has no page to take people from: its notification
        // goes to people picked, from the start, under Notion's "특정
        // 사용자에게 알림 보내기" (seen 2026-11-01).
        const pickedOnly = action.type === "send_notification" && surface === "page_button";
        // Its chip's kind is kept with the action (automationRecipientKindOf); one kept before that had words said people.
        const kind = action.type === "send_notification"
          ? automationRecipientKindOf(action) ?? (pickedOnly || action.message.trim() || action.messageFormula ? "people" : null) : null;
        return <AutomationActionCard key={action.id} id={action.id} owner={owner}
          index={index} count={actions.length} title={t(pickedOnly ? "databaseView:notifyPickedPeople" : labelKey(action.type))} types={types} onMove={move}
          icon={edited ? <span className={styles.actionIcon} data-tone="property"><EditPropertyCardMark property={edited} /></span>
            : gone ? <span className={styles.actionIcon} data-tone="red" data-gone aria-hidden="true">
              <GonePropertyGlyph propertyId={gone} fallback={<SlidersIcon size="small" />} /></span>
            : <AutomationActionIcon type={action.type} warn={markWarned(action)} tip={action.type === "edit_property" ? markTip : undefined} />}
          tip={gone ? t("databaseView:automationGonePropertyTip") : undefined} onEdit={gone ? () => undefined : undefined}
          problemTip={markWarned(action) ? markTip : undefined}
          prefix={action.type === "insert_blocks" ? <AutomationInsertionPosition action={action} number={index+1} onChange={next => replace(action.id,next)} />
            // A chosen database's edit reads "[작업 전체 ⌄] 편집 [T ⌄] 에서", as Notion's (seen 2026-11-03).
            // An archive's reads "보관 [페이지 전체 ⌄] [T ⌄] 에서" (seen 2026-11-15).
            : (action.type === "edit_pages" || action.type === "archive_pages") && action.target.type !== "this_page" && action.target.databaseId
              ? <AutomationEditPagesScope action={action} number={index + 1} properties={properties}
              pages={pages} targets={targets} context={draftContext} onChange={next => replace(action.id, next)} />
            : action.type === "add_page" || action.type === "edit_pages" || action.type === "archive_pages" ? <AutomationTargetDatabasePicker number={index + 1}
              warnEmpty={savedIds.has(action.id)} invalidTip={goneTip}
              value={action.type === "add_page" ? action.databaseId : action.target.databaseId} databases={databases}
              targets={targets.targets} search={targets.search} thisPage={action.type === "add_page" ? undefined : editThisPage(action)}
              onChange={databaseId => replace(action.id, action.type === "add_page" ? { ...action, databaseId } : retargetEditPages(action, databaseId))} />
            : action.type === "open_page" || action.type === "open_form" || action.type === "open_url"
              ? <AutomationOpenTargetPicker action={action} number={index + 1} views={views} database={ownDatabase}
                warn={savedIds.has(action.id) && automationActionProblem(action) !== null}
                onChange={next => replace(action.id, next)} />
            : action.type === "send_slack" ? <AutomationSlackChannelChip connectionId={action.connectionId} channelId={action.channelId} number={index + 1}
              warn={shownProblem(action) !== null} workspaceId={draftContext?.workspaceId}
              onChoose={(connectionId, channelId) => replace(action.id, { ...action, connectionId, channelId })} />
            : action.type === "send_notification" && !pickedOnly ? <AutomationRecipientKindChip kind={kind} number={index + 1}
              warn={!kind && savedIds.has(action.id)} onChoose={next => replace(action.id, automationRecipientsOfKind(action, next))} />
            : undefined}
          suffix={action.type === "add_page" && action.databaseId ? <AutomationAddPageTemplate action={action} number={index + 1}
            templates={targets.targets[action.databaseId]?.templates} onChange={next => replace(action.id, next)} />
            : (action.type === "edit_pages" || action.type === "archive_pages") && action.target.type !== "this_page" && action.target.databaseId ? <>
            <AutomationTargetDatabasePicker number={index + 1} warnEmpty={savedIds.has(action.id)} invalidTip={goneTip} value={action.target.databaseId} databases={databases}
              targets={targets.targets} search={targets.search} thisPage={editThisPage(action)} onChange={databaseId => replace(action.id, retargetEditPages(action, databaseId))} />
            <span className={styles.actionTitleText} data-head-word>{t("databaseView:editPagesIn")}</span>
          </>
            : (action.type === "open_page" && (action.pageId || action.target === "trigger_page")) || (action.type === "open_form" && action.viewId)
            ? <AutomationOpenInChip value={action.openIn} number={index + 1} onChange={openIn => replace(action.id, { ...action, openIn })} /> : undefined}
          heading={action.type === "add_page" || action.type === "edit_pages" || action.type === "archive_pages" || (action.type === "send_notification" && !pickedOnly)
            ? headingAround(t(action.type === "add_page" && targets.targets[action.databaseId]?.tasks ? "databaseView:actionHeadings.add_task" : `databaseView:actionHeadings.${action.type}`, { target: CHIP }))
            : action.type === "open_page" || action.type === "open_form" || action.type === "open_url"
              ? headingAround(t("databaseView:actionHeadings.open", { target: CHIP }))
            : action.type === "send_slack" ? headingAround(t("databaseView:actionHeadings.send_slack", { target: CHIP })) : undefined}
          sentence={action.type === "edit_property" ? <EditPropertySentence action={action} property={edited} properties={properties} surface={surface}
            fault={automationActionFormulaFault(action, properties, namesAt(index))} /> : undefined}
          onOpen={gone ? () => {
            const card = document.querySelector<HTMLElement>(`[data-automation-action="${CSS.escape(action.id)}"]`);
            if (card) setGoneMenu({ id: action.id, propertyId: gone, anchor: card, opener: card.querySelector<HTMLElement>("[data-automation-action-open]") });
          } : action.type === "edit_property" ? () => {
            if (!phone) setCardAnchor(document.querySelector(`[data-automation-action="${CSS.escape(action.id)}"]`)?.getBoundingClientRect() ?? null);
            const picking = !edited || !isDatabaseAutomationEditableProperty(edited);
            // A formula's card opens its formula editor at once (Notion's, seen 2026-11-10).
            setEditing(open ? null : { id: action.id, fresh: false, picking, formula: !picking && action.value.kind === "formula" });
          } : undefined}
          menu={(type, after, controls) => propertyPage(after ? actions[index + 1]?.id : action.id)(type, controls)}
          onRemove={remove} fixed={action.type === "set_page_icon"}
          // A notification's and a page icon's menu has no "복제" (Notion's, seen 2026-11-08 and 2026-11-21).
          onCopy={action.type === "send_notification" || action.type === "set_page_icon" ? undefined : () => void copy(action)}
          onAdd={(type, after) => add(type, after ? actions[index + 1]?.id : action.id)}>
          {action.type === "edit_property" ? open ? open.formula ? <AutomationValueFormula action={action} properties={properties} pages={pages}
            names={automationFormulaNames(t, { surface, variables: earlierAutomationVariables(actions, index), properties })} anchor={phone ? null : cardAnchor}
            onChange={next => replace(action.id, next)} onClose={() => setEditing(null)} />
          : inCardWindow(open.picking ? <div className={editStyles.editor} data-automation-value-window>
            <AutomationPropertyPick properties={unset(action.id)} onClose={() => setEditing(null)}
              onBack={() => { if (open.fresh) remove(); setEditing(null); }}
              onPick={property => {
                replace(action.id, { ...action, propertyId: property.id, value: defaultPropertyValue(property) });
                setEditing({ ...open, picking: false });
              }} />
          </div> : <AutomationEditPropertyEditor action={action} number={index + 1} properties={properties} pages={pages} surface={surface}
            variables={earlierAutomationVariables(actions, index)} onChange={next => replace(action.id, next)}
            onBack={open.fresh ? () => setEditing({ ...open, picking: true }) : undefined} onDone={() => setEditing(null)} />,
          open.picking || wideFor(action.propertyId)) : null
            : action.type === "open_page" || action.type === "open_form" || action.type === "open_url" ? null
            // Until it has a data source, Notion's card is its head alone (seen 2026-11-02), a notification's until it names whom (2026-11-08).
            : action.type === "add_page" && !action.databaseId || action.type === "edit_pages" && !action.target.databaseId || action.type === "archive_pages" ? null
            : action.type === "send_notification" && !kind ? null
            : <AutomationActionFields action={action} index={index} surface={surface} properties={properties}
              propertyLabels={propertyLabels} draftContext={draftContext} draftOwnerId={draftOwnerId} pages={pages} databases={databases} apps={apps}
              targets={targets} saved={savedIds.has(action.id)} variables={earlierAutomationVariables(actions, index)} recipientKind={kind}
              variableNames={automationVariableNamesOf(actions)}
              onChange={next => replace(action.id, next)} />}
        </AutomationActionCard>;
      })}
    </div>
    {goneMenu ? <GonePropertyMenu anchor={goneMenu.anchor} opener={goneMenu.opener} propertyId={goneMenu.propertyId} deleteLabel={t("databaseView:automationDeleteAction")}
      onClose={() => setGoneMenu(null)}
      onDelete={() => latest.current.onChange(currentActions().filter(candidate => candidate.id !== goneMenu.id))} /> : null}
    {pending ? <AutomationValuePopover anchor={pending.anchor} above={pending.above}
      wide={pending.action.type !== "edit_property" || wideFor(pending.action.propertyId)} onClose={finishPending}>
      <AutomationEditPropertyEditor action={pending.action} number={currentActions().length + 1} properties={properties} pages={pages}
        surface={surface} variables={earlierAutomationVariables(actions, actions.length)} onChange={setPendingValue}
        onBack={pending.added ? undefined : () => { setPending(null); setMenuStart("edit_property"); setMenuOpen(true); }}
        onDone={finishPending} />
    </AutomationValuePopover> : null}
    <div ref={addRef} className={styles.addWrap}>
      <AddActionMenu open={menuOpen} onOpenChange={(open) => { setMenuOpen(open); if (!open) setMenuStart(undefined); }} disabled={false} page={propertyPage()}
        startPage={menuStart}
        buttonLabel={t((phone || newWhenEmpty) && !actions.length ? "databaseView:automationNewAction" : "databaseView:addAction")} types={types} onAdd={add} />
      {/* Notion's five webhooks: told when one is left, and why none is offered after. */}
      {webhookCount >= MAX_WEBHOOK_ACTIONS - 1 && automationActionTypesForSurface(surface).includes("send_webhook") ? (
        <p data-automation-webhooks-capacity={webhookCount >= MAX_WEBHOOK_ACTIONS ? "full" : "near"}>
          {t(webhookCount >= MAX_WEBHOOK_ACTIONS ? "databaseView:webhooksFull" : "databaseView:webhooksNear",
            { count: webhookCount, limit: MAX_WEBHOOK_ACTIONS })}
        </p>
      ) : null}
    </div>
  </div>;
}

function AddActionMenu({ open, onOpenChange, disabled, buttonLabel, types, page, startPage, onAdd }: {
  open: boolean; onOpenChange(open: boolean): void; disabled: boolean; buttonLabel: string;
  types: { type: AutomationActionType; label: string }[]; page?: AutomationActionTypePage; startPage?: AutomationActionType;
  onAdd(type: AutomationActionType): void;
}) {
  const buttonRef = useRef<HTMLButtonElement>(null);
  return <>
    <button ref={buttonRef} type="button" className={styles.addButton} aria-expanded={open} aria-haspopup="menu"
      disabled={disabled} onClick={() => onOpenChange(!open)}>
      <Plus size="regular" aria-hidden="true" />{buttonLabel}
    </button>
    {open ? <AutomationActionTypeMenu trigger={buttonRef} types={types} page={page} startPage={startPage} onAdd={onAdd}
      onClose={() => onOpenChange(false)} /> : null}
  </>;
}
