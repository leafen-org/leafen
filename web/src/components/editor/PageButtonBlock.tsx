import { useRef, useState } from "react";
import { useDialog } from "react-aria";
import type { GraphBlock } from "@/contentGraph";
import { api } from "@/api";
import { AutomationActionEditor } from "@/components/automation/AutomationActionEditor";
import { AutomationFloatingMenus } from "@/components/automation/AutomationActionMenus";
import { ButtonConfirmationDialog } from "@/components/automation/ButtonConfirmationDialog";
import { automationActionProblem } from "@/components/automation/automationActionModel";
import { i18next } from "@/i18n";
import { databaseAutomationEditorBounds } from "@/components/database/databaseAutomationPanelModel";
import { ButtonClickIcon, Settings } from "@/icons/leafen";
import { WarningFillIcon } from "@/icons/leafen/automationIcons";
import { buttonActionsRefused, buttonExecutionCanRestart, buttonTargetMissing, buttonRunNoticeText, dispatchButtonClientOutcomes, pageButtonResultFromWire } from "@/lib/buttonExecution";
import { pageHref } from "@/lib/navigation";
import { buttonResultRequiresConfirmation } from "@/lib/buttonExecutionContract";
import { newId } from "@/lib/ids";
import { dataSourcePage } from "@/lib/dataSourcePage";
import { outOfTrashIn } from "@/lib/pageIndex";
import { useOptionalProductToast } from "@/lib/productToast";
import { useRouter } from "@/lib/router";
import type { AutomationAction, DbProperty, Page, PageButtonActionDocument, PropertyConfig } from "@/lib/types";
import { blockItemText } from "./blockItemLabels";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { PageButtonIcon } from "./PageButtonIcon";
import type { InteractiveAutomationExecuteResponse } from "@/interactiveAutomationWire.generated";
import { usePageButtonExecutionState } from "./pageButtonExecutionState";
import { useBlockSetupRequest } from "./blockSetup";
import styles from "./editorStyles.generated.module.css";

function buttonActions(block: GraphBlock): AutomationAction[] {
  const document = block.content.buttonActionDocument as PageButtonActionDocument | undefined;
  return document?.version === 1 && Array.isArray(document.actions) ? document.actions : [];
}

// What the actions may use: the page's own databases, offered first (one on
// another page is searched for in the editor), then the pages one may open —
// none in the trash or under a page in it (outOfTrashIn; a database marked so
// by dataSourcePage) — within the editor's bound.
function actionPages(adapter: NativeEditorGraphAdapter): Page[] {
  const databases = adapter.graph.dataSources.map((source) => dataSourcePage(source, adapter.workspaceId, 0, { parentId: adapter.pageId, parentType: "page" }));
  const listed = adapter.moveContext?.pages ?? [];
  const pages: Page[] = listed.filter(outOfTrashIn(listed)).map((page) => ({
    id: page.id, workspaceId: page.workspaceId, parentId: page.parentId || null, parentType: page.parentId ? "page" : "workspace",
    kind: "page", title: page.title, iconType: "none", position: page.position,
  }));
  return [...databases, ...pages].slice(0, databaseAutomationEditorBounds.maxPages);
}

/** How many of the blocks a run put on the page stand outside the others it put there. */
function topInsertedBlocks(adapter: NativeEditorGraphAdapter, ids: readonly string[]) {
  const inserted = new Set(ids);
  const parents = new Map(adapter.graph.blocks.map((block) => [block.id, block.parentId]));
  return ids.filter((id) => !inserted.has(parents.get(id) ?? "")).length;
}

function actionProperties(adapter: NativeEditorGraphAdapter): DbProperty[] {
  return adapter.graph.properties.slice(0, databaseAutomationEditorBounds.maxProperties).map((property, position) => ({
    id: property.id, databaseId: property.dataSourceId, name: property.name, type: property.type, position,
    config: property.config as PropertyConfig,
  }));
}

/**
 * A page's button, as Notion's (seen 2026-10-06): made from the "/" menu it
 * opens its settings — its name, when it runs and its actions — and the gear
 * beside it opens them again; pressed, the server runs its actions, asking
 * first where one says to, and blocks it adds go right under it.
 */
export function ButtonBlock({ adapter, block }: { adapter: NativeEditorGraphAdapter; block: GraphBlock }) {
  const router = useRouter();
  const toast = useOptionalProductToast();
  const [setupOpen, setSetupOpen] = useState(false);
  // While its settings are open, whether an action shows what it lacks (AutomationActionEditor's onWarnings);
  // null until the editor says, the actions it opened with all asked for by then.
  const [openWarned, setOpenWarned] = useState<boolean | null>(null);
  const closeSetup = () => { setSetupOpen(false); setOpenWarned(null); };
  // A new button's settings open without taking the keys; opened by a press
  // they put them in its name, as Notion's (seen 2026-11-01).
  const [nameKeys, setNameKeys] = useState(true);
  const { running, setRunning, challenge, setChallenge, run } = usePageButtonExecutionState(block.id);
  const name = typeof block.content.label === "string" ? block.content.label : "";
  const label = name.trim() || blockItemText("button.newButton");
  const actions = buttonActions(block);
  const writable = !adapter.readOnly;
  useBlockSetupRequest(block.id, () => {
    if (!writable) return;
    setNameKeys(false);
    setSetupOpen(true);
  });
  const openSetup = () => {
    setNameKeys(true);
    setSetupOpen(true);
  };

  function save(change: { name?: string; actions?: AutomationAction[] }) {
    const live = adapter.graph.blocks.find(({ id }) => id === block.id);
    if (!live || adapter.readOnly || !adapter.isCurrent()) return;
    const nextName = change.name ?? (typeof live.content.label === "string" ? live.content.label : "");
    const nextActions = change.actions ?? buttonActions(live);
    const document: PageButtonActionDocument | undefined = nextActions.length
      ? { version: 1, label: nextName.trim(), actions: nextActions }
      : undefined;
    adapter.updateBlock(live, { label: nextName, buttonActionDocument: document });
    if (change.actions) {
      const kept = new Set(nextActions.flatMap(action => action.type === "insert_blocks" && action.sourceBlockId ? [action.sourceBlockId] : []));
      const removed = buttonActions(live).flatMap(action => action.type === "insert_blocks" && action.sourceBlockId && !kept.has(action.sourceBlockId) ? [action.sourceBlockId] : []);
      if (removed.length) adapter.remove(removed);
    }
  }

  async function execute(confirmationToken?: string, cancel = false) {
    if (!adapter.session || running) return;
    if (!actions.length) {
      if (writable) openSetup();
      return;
    }
    setRunning(true);
    try {
      // The server runs the actions it has, so the ones just set reach it first.
      if (adapter.graphSync && !await adapter.graphSync.flush()) throw new Error("Button edits did not save.");
      if (!adapter.isCurrent() || adapter.signal.aborted || adapter.readOnly) return;
      const executionId = run.current ?? newId();
      run.current = executionId;
      const phase: NonNullable<typeof run.phase> = run.phase ?? { inserted: [], created: 0, edited: 0, tasks: true, first: "", canUndo: false, lastWindow: "", outcomes: [] };
      run.phase = phase;
      const observe = async (response: InteractiveAutomationExecuteResponse) => {
        const result = pageButtonResultFromWire(response, executionId);
        const blocks = result.insertedBlockIds ?? [], pages = result.createdPageIds ?? [], outcomes = result.clientOutcomes ?? [];
        const edited = "editedPageIds" in result ? result.editedPageIds ?? [] : [];
        const window = JSON.stringify([blocks, pages, edited, outcomes]);
        if (window !== phase.lastWindow) {
          phase.inserted.push(...blocks); phase.created += pages.length; phase.edited += edited.length;
          if (pages.length || edited.length) phase.tasks &&= "tasks" in result && result.tasks === true;
          phase.first ||= pages[0] ?? edited[0] ?? "";
          phase.outcomes.push(...outcomes); phase.lastWindow = window;
        }
        phase.canUndo ||= response.canUndo === true;
      };
      const response = await api.executePageButton(adapter.session, adapter.pageId, block.id,
        { executionId, ...(confirmationToken ? { confirmationToken } : {}), ...(cancel ? { cancel: true } : {}) }, adapter.signal, observe);
      await observe(response);
      if (!adapter.isCurrent()) return;
      const result = pageButtonResultFromWire(response, executionId);
      const created = phase.created;
      if (phase.inserted.length || created) {
        await adapter.graphSync?.reread();
        // Pages the run made, such as a draft's database copied below the
        // button, join the page list and sidebar as Notion's do.
        void adapter.moveContext?.onPagesChanged?.()?.catch(() => undefined);
      }
      if (!adapter.isCurrent()) return;
      // The blocks it put on the page are counted as Notion's notice counts
      // them, those inside another it put there not (seen 2026-11-13: a
      // toggle with a line in it, a line and a sub-page, "블록 3개").
      const inserted = topInsertedBlocks(adapter, phase.inserted);
      // Pages it made or changed are what it says, with the first to open, as
      // Notion's (seen 2026-11-03); blocks alone, how many, to undo.
      const pagesNotice = buttonRunNoticeText(created, phase.edited, phase.tasks);
      const first = phase.first;
      if (pagesNotice) {
        toast?.notify(pagesNotice, "default", [
          ...(first ? [{ icon: "open" as const, label: blockItemText("button.open"), onClick: () => router.push(pageHref(first)) }] : []),
          ...(phase.canUndo ? [{ icon: "undo" as const, label: blockItemText("undo"), onClick: async () => {
            if (!adapter.isCurrent() || adapter.readOnly || !adapter.session) return;
            try {
              await api.executePageButton(adapter.session, adapter.pageId, block.id, { executionId, undo: true }, adapter.signal);
              await adapter.graphSync?.reread();
              void adapter.moveContext?.onPagesChanged?.()?.catch(() => undefined);
            } catch { if (adapter.isCurrent()) toast.notify(blockItemText("button.undoFailed"), "error"); }
          } }] : []),
        ]);
      } else if (inserted) {
        toast?.notify(blockItemText("button.inserted", { count: inserted }), "default", phase.canUndo ? {
          label: blockItemText("undo"), onClick: async () => {
            if (!adapter.isCurrent() || adapter.readOnly || !adapter.session) return;
            try {
              await api.executePageButton(adapter.session, adapter.pageId, block.id, { executionId, undo: true }, adapter.signal);
              if (!adapter.isCurrent()) return;
              await adapter.graphSync?.reread();
              if (run.current === executionId) { run.current = null; run.phase = undefined; setChallenge(null); }
              toast.notify(blockItemText("button.undone"));
            } catch { if (adapter.isCurrent()) toast.notify(blockItemText("button.undoFailed"), "error"); }
          },
        } : undefined);
      }
      run.phase = undefined;
      dispatchButtonClientOutcomes(phase.outcomes, { navigate: router.push });
      if (buttonResultRequiresConfirmation(result)) { setChallenge(result); return; }
      run.current = null;
      setChallenge(null);
    } catch (error) {
      if (buttonExecutionCanRestart(error)) { run.current = null; run.phase = undefined; setChallenge(null); }
      // One its server won't run says so, with the way to its settings, as an
      // action lacking what it needs does (2026-11-03: a refused run said nothing).
      if (!adapter.isCurrent() || adapter.signal.aborted) return;
      const fixable = buttonTargetMissing(error) ? "button.targetMissing" : buttonActionsRefused(error) ? "button.refused" : null;
      if (fixable) {
        toast?.notify(blockItemText(fixable), "error", writable ? { label: blockItemText("button.configure"), onClick: openSetup } : undefined);
      } else toast?.notify(blockItemText("button.failed"), "error");
    } finally {
      if (adapter.isCurrent()) setRunning(false);
    }
  }

  // A button with nothing to do, or an action lacking what it needs, says so
  // by a red mark and the first lack's words, and a press opens its settings,
  // as Notion's (seen 2026-10-31, 2026-11-01). While its settings are open
  // the mark stays as long as an action there shows what it lacks — one
  // saved so when they opened — and goes as that is mended; a new button's,
  // one with no action, and an action added there leave it off until they
  // close (Notion's, seen 2026-11-21 reopened in two tabs).
  const actionProblem = actions.map((action) => automationActionProblem(action)).find((key): key is string => Boolean(key));
  const problem = actions.length === 0 ? "blockItem:button.noActions" : actionProblem;
  const idle = Boolean(problem);
  const marked = setupOpen ? openWarned ?? Boolean(actionProblem) : idle;
  return <div className={styles.pageButton} data-page-button>
    <div className={styles.buttonBlock}>
      <button type="button" className={styles.buttonAction} aria-busy={running || undefined}
        data-unnamed={name.trim() ? undefined : "true"} data-icon-tip={problem && marked && writable ? i18next.t(problem) : undefined}
        data-tip-keys={marked && writable ? blockItemText("button.clickToEdit") : undefined}
        disabled={!writable || !adapter.session || running} onClick={() => { if (idle) openSetup(); else void execute(); }}>
        <PageButtonIcon adapter={adapter} block={block} /><span className={styles.buttonLabel}>{label}</span>
        {marked ? <WarningFillIcon className={styles.buttonIdleMark} size="small" aria-hidden="true" /> : null}
      </button>
      {writable ? <button type="button" className={styles.buttonSettings} aria-label={blockItemText("button.configure")}
        aria-expanded={setupOpen} onClick={() => { if (setupOpen) closeSetup(); else openSetup(); }}><Settings size="small" aria-hidden="true" /></button> : null}
    </div>
    {setupOpen && writable ? <ButtonSetup adapter={adapter} block={block} name={name} nameKeys={nameKeys} actions={actions}
      pages={actionPages(adapter)} properties={actionProperties(adapter)}
      onName={(next) => save({ name: next })} onActions={(next) => save({ actions: next })}
      onWarnings={setOpenWarned} onClose={closeSetup} /> : null}
    {challenge ? <ButtonConfirmationDialog challenge={challenge} busy={running}
      onCancel={() => void execute(challenge.confirmationToken, true)}
      onConfirm={() => void execute(challenge.confirmationToken)} /> : null}
  </div>;
}

// The settings open in the page under the button, as Notion's do, and close
// with "Done" or the gear alone: Escape and a press elsewhere leave them
// open, as Notion's (seen 2026-11-04).
function ButtonSetup({ adapter, block, name, nameKeys, actions, pages, properties, onName, onActions, onWarnings, onClose }: {
  adapter: NativeEditorGraphAdapter; block: GraphBlock;
  name: string; nameKeys: boolean; actions: AutomationAction[]; pages: Page[];
  properties: DbProperty[];
  onName(name: string): void; onActions(actions: AutomationAction[]): void; onWarnings(shown: boolean): void; onClose(): void;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const { dialogProps } = useDialog({ "aria-label": blockItemText("button.configure") }, panelRef);
  return <div ref={panelRef} className={styles.buttonConfig} {...dialogProps} data-page-button-setup data-block-panel>
    <div className={styles.buttonConfigTitle}><PageButtonIcon adapter={adapter} block={block} picker /><input className={styles.buttonConfigName} value={name} autoFocus={nameKeys} placeholder={blockItemText("button.newButton")}
      aria-label={blockItemText("button.name")} onFocus={(event) => event.currentTarget.select()} onChange={(event) => onName(event.currentTarget.value)} /></div>
    <div className={styles.buttonConfigBody}>
    <div className={styles.buttonConfigLabel}>{blockItemText("button.when")}</div>
    <div className={styles.buttonConfigWhen}><ButtonClickIcon size="small" aria-hidden="true" />{blockItemText("button.clicked")}</div>
    <div className={styles.buttonConfigLink} aria-hidden="true" />
    <div className={styles.buttonConfigLabel}>{blockItemText("button.actions")}</div>
    {/* Its menus float over the page, as Notion's "작업 추가" list does (seen 2026-11-02), not cut by the setup's bottom. */}
    <AutomationFloatingMenus.Provider value={true}>
    <AutomationActionEditor surface="page_button" bounds={databaseAutomationEditorBounds} actions={actions} newWhenEmpty
      pages={pages} properties={properties} draftContext={adapter} draftOwnerId={block.id}
      readActions={() => { const live = adapter.graph.blocks.find(candidate => candidate.id === block.id); return live ? buttonActions(live) : []; }}
      onWarnings={onWarnings} onChange={onActions} />
    </AutomationFloatingMenus.Provider>
    <button type="button" className={styles.buttonConfigDone} onClick={onClose}>{blockItemText("button.done")}</button>
    </div>
  </div>;
}
