"use client";

import { useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { userErrorText } from "@/lib/settingsErrors";
import { useShownChanges } from "@/lib/shownChanges";
import { useMadeWithoutAnswer } from "@/lib/uncertainWrites";
import type { AutomationAction, DatabaseAutomationDefinition, DbProperty, DbView, Page } from "@/lib/types";
import { databaseAutomationPausedReasons, type DatabaseAutomationScope, type DatabaseAutomationTrigger } from "@/databaseAutomationWire.generated";
import type { DatabaseAutomationService } from "@/databaseAutomationApi";
import { disambiguatePropertyLabels } from "./propertyOptionLabels";
import { automationDocumentActions } from "@/components/automation/AutomationActionEditor";
import { DatabaseAutomationDialog } from "./DatabaseAutomationDialog";
import { PhoneAutomationEditor, PhoneAutomationList } from "./DatabaseAutomationPhone";
import { DesktopAutomationList } from "./DatabaseAutomationList";
import { personName as directoryPersonName } from "@/lib/peopleDirectory";
import { newScheduleTriggerDraft } from "./automationScheduleModel";
import {
  automationScopeOf,
  createDatabaseAutomationPanelLifecycle,
  databaseAutomationEditorBounds,
  defaultEventTriggerDraft,
  isDatabaseAutomationEditableProperty,
  newDefinitionDraft,
  storedDefinitionDraft,
  storedTrigger,
  triggerDraftFromAutomation,
  type AutomationTriggerDraft,
  type DatabaseAutomationPanelOperation,
} from "./databaseAutomationPanelModel";
import { AutomationRelationRowsContext } from "../automation/automationRelationRows";
import { AutomationGonePropertiesContext, automationGonePropertyIds, useAutomationGoneProperties } from "../automation/AutomationGoneProperty";
import type { DatabaseDeletedPropertiesSurface } from "@/lib/databaseDeletedProperties";
import { useProductPages } from "@/components/product/productPagesContext";
import type { WorkspacePerson } from "@/lib/workspacePeopleChoices";
import {
  databaseAutomationBroken,
  databaseAutomationDraftBroken,
  databaseAutomationSummary,
  type DatabaseAutomationReferences,
} from "./databaseAutomationSummary";
import { confirmAction } from "../ConfirmActionDialog";
import { useOptionalProductToast } from "@/lib/productToast";
import { noteDatabaseAutomationCount, useDatabaseAutomationCount } from "./databaseAutomationCount";
import { useAutomationListReadAgain } from "./useAutomationListReadAgain";

// The automations newest first, as Notion's list (seen 2026-11-02).
const newestFirst = (left: DatabaseAutomationDefinition, right: DatabaseAutomationDefinition) =>
  (Date.parse(right.createdAt ?? "") || 0) - (Date.parse(left.createdAt ?? "") || 0) || left.id.localeCompare(right.id);

// Why an automation stopped, which its row says with what to fix. One whose
// actions, view or schedule no longer hold reads "Invalid", as Notion's list
// marks an automation that edits a deleted property (seen 2026-10-14) — and
// so does one naming a property or view its database no longer has, before
// it runs (seen 2026-10-15); a reason the server doesn't name, as before it
// named them, is "unknown".
const INVALID_REASONS: ReadonlySet<string> = new Set(["action", "view", "schedule"]);
function pausedReason(reason: string | null | undefined) {
  return (databaseAutomationPausedReasons as readonly string[]).includes(reason ?? "") ? reason! : "unknown";
}

export function DatabaseAutomationPanel({
  readOnly = false,
  phone = false,
  databaseName = "",
  databaseIcon,
  databaseId,
  automationCount,
  editorPages,
  properties,
  relationRows,
  service,
  views,
  onClose,
  onBack,
  onSettings,
  dockRef,
  currentUserId,
  deletedProperties,
  triggerPropertyIds,
  editsItsPage = true,
  wideSheet = false,
}: {
  /** A reader's, as Notion's (seen 2026-10-12): the list with its search, each opened unchangeable. */
  readOnly?: boolean;
  /** On a phone: the list and the editor as Notion's sheets (DatabaseAutomationPhone). */
  phone?: boolean;
  /** The database's name, which a phone's editor says it applies to. */
  databaseName?: string;
  /** The database's own icon, beside its name where an automation applies. */
  databaseIcon?: ReactNode;
  databaseId: string;
  /** How many the page's read said it has, which the count its list holds is told beside. */
  automationCount?: number;
  editorPages: Page[];
  properties: DbProperty[];
  relationRows?: (dataSourceId: string) => Page[];
  service: DatabaseAutomationService;
  views: DbView[];
  onClose: (restoreFocus?: boolean) => void;
  /** "←" from the list back to the view settings it opened from, as Notion's; a phone's "‹" closes without it. */
  onBack?: () => void;
  /** The view's settings, which a desktop's list gives way to once its last automation is gone (Notion's, seen 2026-11-02). */
  onSettings?: () => void;
  /** Its database's toolbar, which a desktop's list hangs under on the database's page and opens under elsewhere. */
  dockRef?: RefObject<HTMLElement | null>;
  /** Who is making a new one, whose name an unnamed one reads by. */
  currentUserId?: string;
  /** The database's Deleted properties, which name a property an open one still uses and bring it back. */
  deletedProperties?: DatabaseDeletedPropertiesSurface;
  /** The properties a condition may name; unset, any it lists. */
  triggerPropertyIds?: readonly string[];
  /** Whether a step may edit the page it ran for: People's may not. */
  editsItsPage?: boolean;
  /** A touch screen wider than a phone's sheet, whose list has no "‹" but back to settings (Notion's iPad, seen 2026-11-15). */
  wideSheet?: boolean;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const translationRef = useRef(t);
  translationRef.current = t;
  const lifecycleOwner = useRef(createDatabaseAutomationPanelLifecycle());
  const operationRef = useRef<DatabaseAutomationPanelOperation | null>(null);
  // The list's search, where an editor closing over it hands the keys back.
  const listSearchRef = useRef<HTMLInputElement>(null);
  const mutationBusyRef = useRef(false);
  const [automations, setAutomations] = useState<DatabaseAutomationDefinition[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<DatabaseAutomationDefinition | "new" | null>(null);
  const [draftName, setDraftName] = useState("");
  const [draftTrigger, setDraftTrigger] = useState<AutomationTriggerDraft>(defaultEventTriggerDraft);
  const [draftActions, setDraftActions] = useState<AutomationAction[]>([]);
  // Where it applies: the database, or one of its views (a phone picks it, as Notion's "적용할 페이지 위치").
  const [draftScope, setDraftScope] = useState<DatabaseAutomationScope>({ type: "database" });
  // The draft as it began: a phone's "←" asks before leaving changes behind, as Notion's.
  const [began, setBegan] = useState<string | null>(null);
  const editableProperties = useMemo(
    () => properties.filter(isDatabaseAutomationEditableProperty),
    [properties],
  );
  const triggerProperties = useMemo(() => triggerPropertyIds ? properties.filter(({ id }) => triggerPropertyIds.includes(id)) : properties,
    [properties, triggerPropertyIds]);
  const triggerPropertyLabels = useMemo(() => disambiguatePropertyLabels(properties, t("databaseView:untitled")), [properties, t]);
  const draftSurface = draftTrigger.type === "schedule" ? "schedule_automation" : "event_automation";
  const serializedDraftTrigger = storedTrigger(draftTrigger, properties);
  const references = useMemo<DatabaseAutomationReferences>(
    () => ({ databaseId, properties, views }),
    [databaseId, properties, views],
  );
  const draftBroken = editing !== null && databaseAutomationDraftBroken(
    draftTrigger,
    draftActions,
    draftScope.type === "view" ? draftScope.viewId ?? "" : null,
    references,
  );
  // The deleted properties an open one names: their names and marks, and "속성 복원" (AutomationGoneProperty).
  const goneIds = editing === null ? [] : automationGonePropertyIds(
    draftTrigger.type === "events" ? draftTrigger.conditions.flatMap((condition) => condition.type === "property_edited" ? [condition.propertyId] : []) : [],
    draftActions, properties);
  const gone = useAutomationGoneProperties(deletedProperties, goneIds,
    (reason) => setError(userErrorText(reason, translationRef.current("databaseView:automationRestorePropertyFailed"))));
  // The people a summary names: a notification's or a person value's.
  const loadPeople = useProductPages()?.workspacePeople;
  const [people, setPeople] = useState<WorkspacePerson[]>([]);
  // Its people name recipients and, for one left unnamed, its maker; a new one's too.
  const needsPeople = automations.length > 0 || editing !== null;
  useEffect(() => {
    if (!loadPeople || !needsPeople) return;
    const controller = new AbortController();
    loadPeople(controller.signal).then((result) => { if (!controller.signal.aborted) setPeople(result); }, () => {});
    return () => controller.abort();
  }, [needsPeople, loadPeople]);
  const personName = (id: string) => {
    const person = people.find((candidate) => candidate.id === id);
    return person ? (person.me ? t("databaseView:recipientMe") : person.name) : null;
  };

  useEffect(() => {
    const operation = lifecycleOwner.current.begin(service.identity);
    operationRef.current = operation;
    mutationBusyRef.current = false;
    setAutomations([]);
    setEditing(null);
    setBusyId(null);
    setLoading(true);
    setError("");
    // With none yet, it opens on a new one, as Notion's does — for an editor:
    // at once where the page's read said none (a press answers on the next
    // frame), else once the list says so, a draft begun meanwhile kept.
    if (automationCountRef.current === 0 && !readOnlyRef.current) beginCreateRef.current();
    void service.list(operation.signal)
      .then((result) => {
        if (!operation.isCurrent()) return;
        setAutomations([...result].sort(newestFirst));
        if (!result.length && !readOnlyRef.current && editingRef.current === null) beginCreateRef.current();
      })
      .catch((reason: unknown) => {
        if (!operation.isCurrent()) return;
        setError(userErrorText(reason, translationRef.current("databaseView:automationLoadFailed")));
      })
      .finally(() => {
        if (operation.isCurrent()) setLoading(false);
      });
    return () => {
      operation.retire();
      if (operationRef.current === operation) operationRef.current = null;
    };
  }, [service]);

  // The toolbar's "⚡" follows the list: blue once one is made, grey once the
  // last goes — told as the list changes, as the panel may close with it.
  useEffect(() => {
    if (!loading && !error) noteDatabaseAutomationCount(databaseId, automations.length, automationCount);
  }, [automationCount, automations.length, databaseId, error, loading]);
  const listRef = useRef(automations);
  listRef.current = automations;
  const changeList = (change: (items: DatabaseAutomationDefinition[]) => DatabaseAutomationDefinition[]) => {
    const next = change(listRef.current);
    listRef.current = next;
    setAutomations(next);
    noteDatabaseAutomationCount(databaseId, next.length, automationCount);
  };

  // Each one as last turned on or off here, over what the server holds.
  const turns = useShownChanges<Record<string, DatabaseAutomationDefinition["status"]>>();
  const turning = useRef(new Map<string, Promise<unknown>>());
  const shownAutomation = (automation: DatabaseAutomationDefinition): DatabaseAutomationDefinition => {
    const status = turns.shown[automation.id];
    return status && status !== automation.status ? { ...automation, status, enabled: status !== "disabled" } : automation;
  };
  const shownAutomations = automations.map(shownAutomation);
  // The one the server last answered for, whose revision a write goes on.
  const latestOf = (automation: DatabaseAutomationDefinition) => listRef.current.find(({ id }) => id === automation.id) ?? automation;

  // Read again, leaving one being edited as it is, once a write without the
  // server's answer may have been taken after all, or the page's room tells
  // of a change made elsewhere.
  useAutomationListReadAgain(service, mutationBusyRef, (result) => {
    setAutomations([...result].sort(newestFirst));
    setError("");
  });
  // A new one whose save went without the answer and that the list, read
  // again, holds was made: its editor closes as the answer would have closed
  // it, rather than wait to make it again (lib/uncertainWrites.ts).
  const editingRef = useRef(editing);
  editingRef.current = editing;
  const makeNew = useMadeWithoutAnswer({
    items: automations,
    idOf: (automation) => automation.id,
    landed: (made) => {
      if (editingRef.current !== "new") return;
      madeNew(made, listRef.current.length <= 1);
    },
  });
  // A new one turned on closes its window and says so, as Notion's (seen
  // 2026-11-02); the first, alone in its window, takes the panel with it, and
  // one made from the list leaves the list open with it in, the keys on the
  // page (Notion's, seen 2026-11-15 in two tabs).
  const madeLeavesFocus = useRef(false);
  const madeNew = (made: DatabaseAutomationDefinition, alone: boolean) => {
    madeLeavesFocus.current = !alone;
    setEditing(null);
    toast?.notify(t("databaseView:automationCreated", { name: nameOf(made) }), "success");
    if (alone) onClose();
  };

  // An automation left unnamed reads as its maker's, as Notion's "〈사람〉님의 자동화 기능" (seen 2026-10-24) — at once from the names the app knows, not first "자동화" until the workspace's people come back (a phone, 2026-11-15).
  const nameBy = (userId?: string) => {
    const person = userId ? directoryPersonName(userId) || people.find((candidate) => candidate.id === userId)?.name || "" : "";
    return person ? t("databaseView:automationDefaultName", { name: person }) : t("databaseView:automationUnnamed");
  };
  const toast = useOptionalProductToast();
  const nameOf = (automation: DatabaseAutomationDefinition) => automation.name || nameBy(automation.createdBy);
  // Back where the list opened from: the view settings, else closed.
  const back = onBack ?? onClose;
  const draftPlaceholder = nameBy(editing === "new" ? currentUserId : editing?.createdBy);

  function beginCreate() {
    madeLeavesFocus.current = false;
    // A new one has no name, as Notion's, and shows its maker's in grey.
    const name = "";
    // A new automation starts with no condition and no action, as Notion's dialog and sheet; two or more conditions start as "모든".
    const trigger: AutomationTriggerDraft = { type: "events", mode: "all", conditions: [] };
    const actions: AutomationAction[] = [];
    setEditing("new");
    setDraftName(name);
    setDraftTrigger(trigger);
    setDraftActions(actions);
    setDraftScope({ type: "database" });
    setBegan(JSON.stringify([name, trigger, actions, { type: "database" }]));
    setError("");
  }

  const beginCreateRef = useRef(beginCreate), readOnlyRef = useRef(readOnly);
  beginCreateRef.current = beginCreate; readOnlyRef.current = readOnly;
  // How many it has as its list last held here, ahead of the page's next read: none made opens on a new one.
  const automationCountRef = useRef(automationCount);
  automationCountRef.current = useDatabaseAutomationCount(databaseId, automationCount);

  function beginEdit(automation: DatabaseAutomationDefinition) {
    const trigger = triggerDraftFromAutomation(automation);
    const actions = automationDocumentActions(automation.actionDocument, databaseAutomationEditorBounds);
    setEditing(automation);
    setDraftName(automation.name);
    setDraftTrigger(trigger);
    setDraftActions(actions);
    setDraftScope(automationScopeOf(automation));
    setBegan(JSON.stringify([automation.name, trigger, actions, automationScopeOf(automation)]));
    setError("");
  }

  async function runMutation(
    automationId: string,
    mutate: (operation: DatabaseAutomationPanelOperation) => Promise<void>,
    fallbackKey: string,
  ) {
    const operation = operationRef.current;
    if (!operation?.isCurrent() || mutationBusyRef.current) return;
    mutationBusyRef.current = true;
    setBusyId(automationId);
    setError("");
    try {
      await turning.current.get(automationId)?.catch(() => undefined);
      if (!operation.isCurrent()) return;
      await mutate(operation);
    } catch (reason) {
      if (!operation.isCurrent()) return;
      setError(userErrorText(reason, t(fallbackKey)));
    } finally {
      if (operation.isCurrent()) {
        mutationBusyRef.current = false;
        setBusyId(null);
      }
    }
  }

  async function saveDraft() {
    const name = draftName.trim();
    if (!editing || !serializedDraftTrigger) return;
    const current = editing;
    // A recurring one applies to the database, as Notion's "〈DB〉에 적용".
    const scope: DatabaseAutomationScope = serializedDraftTrigger.type === "schedule" ? { type: "database" } : draftScope;
    await runMutation(current === "new" ? "new" : current.id, async (operation) => {
      const draft = current === "new"
        ? newDefinitionDraft(name, serializedDraftTrigger, draftActions, scope)
        : storedDefinitionDraft(latestOf(current), name, serializedDraftTrigger, draftActions, scope);
      const result = current === "new"
        ? await makeNew(() => service.create(draft, operation.signal))
        : await service.update(latestOf(current), draft, operation.signal);
      if (!operation.isCurrent()) return;
      const alone = !listRef.current.length;
      changeList((items) => (
        [...items.filter((automation) => automation.id !== result.id), result]
          .sort(newestFirst)
      ));
      if (current === "new") madeNew(result, alone); else setEditing(null);
    }, "databaseView:automationSaveFailed");
  }

  // "사용 중" turns it on or off as it is pressed, the list free all the while
  // (user decision 2026-11-12: a press answers on the next frame); each
  // automation's turns go in the order pressed, each on the revision the one
  // before left, and one the server refuses comes back as the server has it
  // with why (lib/shownChanges.ts). A save or delete of it waits for them.
  function toggleAutomation(automation: DatabaseAutomationDefinition) {
    const operation = operationRef.current;
    if (!operation?.isCurrent()) return;
    const on = shownAutomation(automation).status !== "active";
    setError("");
    const sent = turns.change({ [automation.id]: on ? "active" : "disabled" }, async () => {
      const latest = latestOf(automation);
      const result = on && latest.status === "paused"
        ? await service.resume(latest, operation.signal)
        : await service.setEnabled(latest, on, operation.signal);
      if (!operation.isCurrent()) return;
      changeList((items) => items.map((item) => item.id === automation.id ? result : item));
      // A phone's open editor turns it on or off at once, as Notion's "활성".
      setEditing((open) => open !== "new" && open?.id === automation.id ? result : open);
    }, automation.id);
    turning.current.set(automation.id, sent);
    const done = () => { if (turning.current.get(automation.id) === sent) turning.current.delete(automation.id); };
    void sent.then(done, done);
    void sent.catch((reason: unknown) => {
      if (operation.isCurrent()) setError(userErrorText(reason, t("databaseView:automationSaveFailed")));
    });
  }

  // "복제", as Notion's: the same automation under the same name, running.
  async function duplicateAutomation(automation: DatabaseAutomationDefinition) {
    const actions = automationDocumentActions(automation.actionDocument, databaseAutomationEditorBounds);
    await runMutation(automation.id, async (operation) => {
      const result = await service.create(storedDefinitionDraft(automation, automation.name,
        automation.trigger as unknown as DatabaseAutomationTrigger, actions), operation.signal);
      if (!operation.isCurrent()) return;
      changeList((items) => [...items.filter((item) => item.id !== result.id), result].sort(newestFirst));
    }, "databaseView:automationSaveFailed");
  }

  // Its "삭제" filled red, as Notion's (seen 2026-11-08).
  async function deleteAutomation(automation: DatabaseAutomationDefinition) {
    if (!await confirmAction({ message: t("databaseView:confirmDeleteAutomation", { name: nameOf(automation) }),
      confirmLabel: t("common:actions.delete"), danger: "filled" })) return;
    await runMutation(automation.id, async (operation) => {
      await service.remove(latestOf(automation), operation.signal);
      if (!operation.isCurrent()) return;
      changeList((items) => items.filter((item) => item.id !== automation.id));
      if (editing !== "new" && editing?.id === automation.id) setEditing(null);
      // The last one gone leaves no list, as Notion's: a phone goes back to
      // where it was opened from, a desktop to the view's settings.
      if (automations.length === 1 && automations[0]?.id === automation.id) (phone ? back : onSettings ?? back)();
    }, "databaseView:automationDeleteFailed");
  }

  const cancelEditing = () => editing === "new" && !automations.length ? (phone || onBack ? back() : onClose(false)) : setEditing(null);
  const changed = JSON.stringify([draftName, draftTrigger, draftActions, draftScope]) !== began;
  // Leaving with changes asks first, as Notion's dialog and sheet do, its
  // "나가기" filled red (seen 2026-11-08).
  const leaveEditor = () => void (async () => {
    if (!readOnly && changed && !await confirmAction({ discard: true,
      message: t("databaseView:automationLeaveUnsaved"), confirmLabel: t("databaseView:automationLeave") })) return;
    cancelEditing();
  })();
  const selectSchedule = () => { setDraftTrigger(newScheduleTriggerDraft()); setDraftScope({ type: "database" }); };
  const statusOf = (automation: DatabaseAutomationDefinition) => {
    const reason = automation.status === "paused" ? pausedReason(automation.pausedReason) : null;
    if (databaseAutomationBroken(automation, references) || (reason !== null && INVALID_REASONS.has(reason))) {
      return { label: t("databaseView:automationInvalid"), invalid: true };
    }
    return automation.status === "active" ? null : { label: t(reason ? "databaseView:automationPaused" : "databaseView:automationDisabled"), invalid: false };
  };
  const actionsOf = (automation: DatabaseAutomationDefinition) => automationDocumentActions(automation.actionDocument, databaseAutomationEditorBounds);
  const summaryOf = (automation: DatabaseAutomationDefinition) => databaseAutomationSummary(automation, references, t, personName, relationRows);
  const onNew = readOnly || editableProperties.length === 0 ? undefined : beginCreate;
  if (phone) {
    return editing ? (
      <AutomationRelationRowsContext.Provider value={relationRows ?? null}><AutomationGonePropertiesContext.Provider value={gone}>
        <PhoneAutomationEditor isNew={editing === "new"} changed={changed} readOnly={readOnly} broken={draftBroken} busy={busyId !== null}
          databaseName={databaseName} databaseIcon={databaseIcon} draftActions={draftActions} draftName={draftName} namePlaceholder={draftPlaceholder} draftSurface={draftSurface} draftTrigger={draftTrigger}
          draftScope={draftScope} setDraftScope={setDraftScope}
          // A saved one that is broken has no "활성" to turn, as the desktop's and Notion's phone (seen 2026-11-11).
          active={editing !== "new" && !databaseAutomationBroken(editing, references) ? shownAutomation(editing).status === "active" : null}
          onToggleActive={editing !== "new" ? () => void toggleAutomation(editing) : undefined}
          editorPages={editorPages} properties={properties} triggerProperties={triggerProperties} editsItsPage={editsItsPage}
          serializedDraftTrigger={serializedDraftTrigger} triggerPropertyLabels={triggerPropertyLabels} views={views} onSave={saveDraft}
          setDraftActions={setDraftActions} setDraftName={setDraftName} setDraftTrigger={setDraftTrigger}
          selectSchedule={selectSchedule} onBack={leaveEditor} />
      </AutomationGonePropertiesContext.Provider></AutomationRelationRowsContext.Provider>
    ) : (
      <PhoneAutomationList automations={shownAutomations} loading={loading} readOnly={readOnly} busy={busyId !== null} error={error} nameOf={nameOf}
        triggerOf={triggerDraftFromAutomation} actionsOf={actionsOf} statusOf={statusOf} summaryOf={summaryOf}
        properties={properties} onNew={onNew}
        onOpen={beginEdit} onToggle={(automation) => void toggleAutomation(automation)} onDelete={(automation) => void deleteAutomation(automation)}
        onDuplicate={(automation) => void duplicateAutomation(automation)}
        onClose={onClose} onBack={wideSheet ? onBack : back} />
    );
  }

  // On a desktop an automation opens in Notion's dialog in the middle
  // (DatabaseAutomationDialog), the list staying under it as Notion's does
  // (seen 2026-11-07): the page beside it doesn't widen and narrow again.
  // The first, with none yet, opens in the dialog alone, no list under it nor
  // left as it closes, as Notion's "⚡" (seen 2026-11-15 in two tabs) — nor
  // drawn for the moment before it opens, to take the keys from its opener:
  // from the view settings, Escape gives them back to its "자동화", ringed, as
  // Notion's (seen in two tabs, 2026-11-15); from "⚡", to the page (seen twice).
  const first = !automations.length && (editing === "new" || editing === null && loading && !readOnly && automationCountRef.current === 0);
  const list = first ? null : (
    <DesktopAutomationList automations={shownAutomations} loading={loading} readOnly={readOnly} automationCount={automationCount} busy={busyId !== null} error={error} nameOf={nameOf}
      noteOf={(automation) => turns.note(automation.id)}
      statusOf={statusOf} summaryOf={summaryOf} triggerOf={triggerDraftFromAutomation} actionsOf={actionsOf} properties={properties}
      reasonOf={(automation) => automation.status === "paused" ? t(`databaseView:automationPausedReason.${pausedReason(automation.pausedReason)}`) : null}
      dockRef={dockRef} searchRef={listSearchRef} onNew={onNew} onOpen={beginEdit} onToggle={(automation) => void toggleAutomation(automation)}
      onDuplicate={(automation) => void duplicateAutomation(automation)} onDelete={(automation) => void deleteAutomation(automation)}
      onClose={onClose} onBack={onBack} />
  );
  if (!editing) return list;
  return (<>
    {list}
    <AutomationRelationRowsContext.Provider value={relationRows ?? null}><AutomationGonePropertiesContext.Provider value={gone}>
      <DatabaseAutomationDialog isNew={editing === "new"} changed={changed} readOnly={readOnly} broken={draftBroken} busy={busyId !== null}
        invalid={editing !== "new" && databaseAutomationBroken(editing, references)}
        error={error} databaseName={databaseName} databaseIcon={databaseIcon} draftActions={draftActions} draftName={draftName} namePlaceholder={draftPlaceholder}
        draftSurface={draftSurface} draftTrigger={draftTrigger} draftScope={draftScope} setDraftScope={setDraftScope}
        active={editing !== "new" ? shownAutomation(editing).status === "active" : null}
        onToggleActive={editing !== "new" ? () => void toggleAutomation(editing) : undefined}
        editorPages={editorPages} properties={properties} triggerProperties={triggerProperties} editsItsPage={editsItsPage}
        serializedDraftTrigger={serializedDraftTrigger} triggerPropertyLabels={triggerPropertyLabels} views={views} onClose={leaveEditor} onSave={saveDraft}
        setDraftActions={setDraftActions} setDraftName={setDraftName} setDraftTrigger={setDraftTrigger} selectSchedule={selectSchedule}
        returnFocusRef={listSearchRef} restoresFocus={() => !madeLeavesFocus.current && !(first && !onBack)} />
    </AutomationGonePropertiesContext.Provider></AutomationRelationRowsContext.Provider>
  </>);
}
