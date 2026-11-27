import { restoreDatabaseProperty } from "@/lib/databaseDeletedProperties";
import type { ProductStatusSink } from "@/lib/productStatus";
import { i18next } from "@/i18n";
import { useEffect, useMemo, useRef, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import { api } from "@/api";
import type { DatabasePropertyConversion } from "@/databasePropertyConversionWire.generated";
import { graphViewUpdates, type ContentGraph, type GraphOperation, type GraphView } from "@/contentGraph";
import { viewsShownAgain, viewsShownBy } from "@/lib/viewShownFallback";
import { graphViewStepCommand, graphViewStepOf, graphViewStepPages } from "@/lib/graphViewSteps";
import type { GraphOperationChange } from "@/lib/nativeGraphAuthority";
import { sayGraphChangeRefusal } from "@/lib/graphChangeRefusal";
import { changesShowOtherSources, collectAuthenticatedContentGraph as collectGraph, collectContentGraphChanges, withChangedRows } from "@/lib/authenticatedContentGraph";
import { createNativeContentGraphWriteCoordinator } from "@/lib/nativeContentGraphWriteCoordinator";
import { sendContentGraphChange } from "@/lib/contentGraphSend";
import { advanceGraphRevision } from "@/lib/graphRevision";
import { keptDatabaseStartRefusal } from "@/lib/databaseStartRefusal";
import type { NativeGraphMutationOutbox } from "@/lib/nativePageMutationBrowser";
import { keptChangeRefusedMessage } from "@/lib/nativePageMutationFailure";
import type { ToastAction } from "@/lib/toastStackView";
import type { Session } from "@/types";
import { productLockRefusalReason } from "@/lib/productErrorPresentation";
import { dependencyShiftNoticesLeave, sayDependencyShift } from "@/lib/dependencyShiftNotice";
import { noteRowPropertiesRedone } from "@/components/database/rowPropertyRedo";
import { relatedChoicesOf, type RelatedChoices } from "@/lib/relatedPropertyDeletion";
import { withCopiesOnTheirWay } from "@/lib/copiesOnTheirWay";
import { advanceContentGraphWriteScope, matchesContentGraphWriteScope, type ContentGraphWriteScope } from "./contentGraphWriteScope";

// After a property type change converts cells, the editor offers to undo
// it for the recovery period.
export type PropertyConversionNotice = {
  notify: (message: string, tone?: "default" | "success" | "error", action?: ToastAction) => void;
  labels: { changed: string; undo: string; restored: string; restoreFailed: string };
};

export function useContentGraphWrites({
  conversionNotice,
  currentPageId,
  readGraph,
  graphRevision,
  mutationOutbox,
  onAccess,
  onDateCommand,
  onPropertyConversion,
  session,
  setGraph,
  setMessage,
  settleToolbarDataSources,
  onBodyEdited,
}: {
  /** A write made here, which tells whether it edits its page (usePageBodyEditing). */
  onBodyEdited?: (pageId: string, operations: readonly GraphOperation[]) => void;
  conversionNotice?: PropertyConversionNotice;
  currentPageId: string | null;
  /** The page's graph as drawn, which writes read as they are made. */
  readGraph: () => ContentGraph;
  graphRevision: MutableRefObject<number>;
  /**
   * The account's outbox, which its writes are sent through
   * (lib/contentGraphSend): they are kept in it as they are made, and a read
   * of a page waits for those it holds.
   */
  mutationOutbox?: NativeGraphMutationOutbox;
  /** Every complete reread refreshes the editor's current native authority. */
  onAccess: (canEdit: boolean, canSuggest: boolean) => void;
  onDateCommand?: (pageId: string, mutationId: string) => void;
  /** A type change that converted cells, as a step of the page's ⌘Z. */
  onPropertyConversion?: (key: string, step: (forward: boolean) => Promise<boolean>) => void;
  session: Session;
  setGraph: Dispatch<SetStateAction<ContentGraph>>;
  setMessage: ProductStatusSink;
  settleToolbarDataSources: (operations: readonly GraphOperation[]) => void;
}) {
  const bodyEdited = useRef(onBodyEdited);
  bodyEdited.current = onBodyEdited;
  // Each write — sent at once or gathered first (a table's cells as they are
  // typed) — may edit its page (graphWritePageEdit.ts).
  const noteBodyEdit = (pageId: string, operations: readonly GraphOperation[]) => bodyEdited.current?.(pageId, operations);
  const scopeRef = useRef<ContentGraphWriteScope>({
    generation: 0,
    pageId: null,
    principalId: "",
    sessionIdentity: "",
  });
  // Writes read the graph the user saw when they changed it.
  const readRef = useRef(readGraph);
  readRef.current = readGraph;
  const graphRef = useRef({ get current() { return readRef.current(); } }).current;
  scopeRef.current = advanceContentGraphWriteScope(scopeRef.current, {
    pageId: currentPageId,
    principalId: session.user.id,
    sessionIdentity: session.token,
  });
  // The page left for another takes the notices of dates moved on it with it.
  useEffect(() => () => dependencyShiftNoticesLeave(currentPageId), [currentPageId]);
  return useMemo(() => {
    const isCurrentPage = (pageId: string, generation: number) => (
      matchesContentGraphWriteScope(scopeRef.current, generation, pageId)
    );
    const isCurrentGeneration = (generation: number) => scopeRef.current.generation === generation;
    // The page whose ⌘Z takes a write's steps: the one it went to, while shown, or the page shown
    // for one sent to the source of a database it shows linked, as Notion's (seen 2026-11-18).
    const stepPageOf = (pageId: string, generation: number) => {
      const shown = scopeRef.current.pageId;
      if (isCurrentPage(pageId, generation)) return pageId;
      return shown && isCurrentPage(shown, generation) && graphRef.current.dataSources.some((source) => source.pageId === pageId) ? shown : null;
    };
    // A type change made again by ⌘⇧Z waits here for the conversions it
    // makes, which its step takes back next; it is not another step or notice.
    const remade = new Map<string, (conversions: DatabasePropertyConversion[]) => void>();
    // Properties a write makes, known before it shows; and whether a step of
    // making or deleting one is being taken, which is no new step itself.
    const madeProperties = new Set<string>();
    let propertyStepping = 0;
    const steppingProperties = async (step: () => Promise<boolean>) => {
      propertyStepping += 1;
      try { return await step(); } finally { propertyStepping -= 1; }
    };
    // A property deleted comes back with ⌘Z as a type change does, and one
    // made goes with it, as Notion's (seen 2026-11-04, 2026-11-06): a deletion
    // as it is sent (commit), one made once the write is taken, saved at once
    // or sent on by the outbox.
    // Written on the page holding them (`holder`: a linked database's on its source's), shown on `pageId`.
    const propertySteps = (pageId: string, generation: number) => {
      // Gone from the screen at once, as a property deleted from its menu is.
      // A two-way relation goes again as it went, with or without its other side.
      const deleteProperties = (ids: readonly string[], related: RelatedChoices = {}, holder = pageId) => {
        if (isCurrentPage(pageId, generation)) {
          setGraph((current) => ({ ...current, properties: current.properties.filter((property) => !ids.includes(property.id)) }));
        }
        return coordinator.commit(holder, ids.map((id) => ({
          kind: "deleteProperty", propertyDeletion: related[id] ? { id, relatedProperty: related[id] } : { id },
        })));
      };
      // A view that took its fallback once shown meanwhile groups or dates
      // by the property again, as one ⌘Z gave back both in Notion (seen
      // 2026-11-08); `shown` are the views on it before it went.
      const restoreProperties = async (ids: readonly string[], shown: readonly GraphView[] = [], holder = pageId) => {
        for (const id of ids) await restoreDatabaseProperty(session, holder, id);
        const now = isCurrentPage(pageId, generation) ? await rereadGraph(pageId) : null;
        const again = now ? viewsShownAgain(shown, now.views) : [];
        if (!now || !again.length) return true;
        setGraph((current) => ({ ...current, views: current.views.map((view) => again.find(({ id }) => id === view.id) ?? view) }));
        await coordinator.commit(pageId, graphViewUpdates(now, again.map((view) => ({ kind: "upsertView" as const, view }))));
        return true;
      };
      return { deleteProperties, restoreProperties };
    };
    // Deletions stepped as they were sent (commit), which their saving doesn't step again.
    const deletionsStepped = new Set<string>();
    let stepsSent = 0;
    const recordPropertySteps = (pageId: string, operations: readonly GraphOperation[], generation: number, stamp: string) => {
      const deleted = operations.flatMap((operation) => (
        operation.kind === "deleteProperty" && operation.propertyDeletion ? [operation.propertyDeletion.id] : []
      )).filter((id) => !deletionsStepped.delete(id));
      const made = operations.flatMap((operation) => (
        operation.kind === "upsertProperty" && madeProperties.delete(operation.property.id) ? [operation.property.id] : []
      ));
      const stepPage = propertyStepping ? null : stepPageOf(pageId, generation);
      if (!stepPage) return;
      const { deleteProperties, restoreProperties } = propertySteps(stepPage, generation);
      if (deleted.length) {
        const shown = viewsShownBy(graphRef.current.views, deleted);
        const related = relatedChoicesOf(operations);
        onPropertyConversion?.(`property-delete/${deleted.join(",")}/${stamp}`, (forward) => steppingProperties(() => (
          forward ? deleteProperties(deleted, related, pageId) : restoreProperties(deleted, shown, pageId))));
      }
      if (made.length) {
        onPropertyConversion?.(`property-create/${made.join(",")}/${stamp}`, (forward) => steppingProperties(async () => {
          if (!forward) return deleteProperties(made, {}, pageId);
          const restored = await restoreProperties(made, [], pageId);
          if (restored) noteRowPropertiesRedone(made);
          return restored;
        }));
      }
    };
    const conversionKey = (pageId: string, propertyIds: readonly string[]) => `${pageId}/${[...propertyIds].sort().join(",")}`;
    const coordinator = createNativeContentGraphWriteCoordinator({
      captureGeneration: () => scopeRef.current.generation,
      onFailure: async (error, failedPageId, operations, generation, wordsRode) => {
		if (!isCurrentGeneration(generation)) return;
        settleToolbarDataSources(operations);
        // A property change the server refused says why; text refused with a block change, its room (graphChangeRefusal.ts).
        sayGraphChangeRefusal(setMessage, error, operations, wordsRode, failedPageId);
		const visiblePageId = scopeRef.current.pageId;
		if (!visiblePageId) return;
        const refreshed = await collectGraph(
          session,
			visiblePageId,
			(next) => { if (isCurrentGeneration(generation)) setGraph(next); },
			() => isCurrentGeneration(generation),
        );
        if (refreshed && isCurrentPage(visiblePageId, generation)) {
          graphRevision.current = refreshed.snapshotRevision;
          onAccess(refreshed.canEdit !== false, refreshed.canSuggest === true);
        }
      },
      onKept: (pageId, operations, generation, outcome, mutationId) => {
        if (!isCurrentGeneration(generation)) return;
        settleToolbarDataSources(operations);
        recordPropertySteps(pageId, operations, generation, mutationId);
        setMessage(i18next.t(outcome === "kept" ? "syncStatusBadge:status.offlineChangesSaved" : "syncStatusBadge:status.contentSaved"));
        // A date edit kept locally can be undone behind it in the same outbox.
        // The server supplies the actual inverse when these writes reach it.
        if (operations.some((operation) => operation.kind === "upsertRow"
          && graphRef.current.rows.some((row) => row.id === operation.row.id)
          && Object.values(operation.row.values).some((value) => value.type === "date"))) onDateCommand?.(pageId, mutationId);
      },
      ...(mutationOutbox ? { pagesWaiting: mutationOutbox.graphPagesWaiting } : {}),
      onQueued: (_pageId, generation) => {
		if (isCurrentGeneration(generation)) setMessage(i18next.t("syncStatusBadge:status.contentSavePending"));
      },
      onSaved: (pageId, operations, receipt, generation) => {
		if (!isCurrentGeneration(generation)) return;
		if (isCurrentPage(pageId, generation)) advanceGraphRevision(graphRevision, receipt.graphRevision);
        settleToolbarDataSources(operations);
        setMessage(i18next.t("syncStatusBadge:status.contentSaved"));
        if (receipt.dateCommand) onDateCommand?.(pageId, receipt.dateCommand);
        // Copies of a deleted synced original stayed on pages this person
        // can't change, as "Unsync all" says of its own.
        if (receipt.keptSyncedCopies) {
          conversionNotice?.notify(i18next.t("blockItem:synced.unsyncAllKept", { count: receipt.keptSyncedCopies }), "error");
        }
        // This receipt may include peer content the optimistic writer never
        // saw. Its cursor alone does not mean that local projection is current.
        if (receipt.mergedBlockContent && isCurrentPage(pageId, generation)) void reread(pageId);
        // The server moved dependent tasks' dates with this one, or with a
        // link it drew: they show at once, said so with a step back that
        // takes the link back too, as Notion's notice (seen 2026-11-02,
        // "종속 작업 1개의 날짜를 이동했습니다. 실행 취소", its step back bare
        // of an arrow); what it changed at a relation's or a dependency's
        // other end shows too. Rows this person may not change stayed, which
        // the notice adds; with none moved it says only that, with no step
        // back, as Notion's. The step back is this page's, while it is shown
        // and its history holds the step (sayDependencyShift).
        if (receipt.dependencyShifts && isCurrentPage(pageId, generation)) {
          void reread(pageId);
          const command = receipt.dateCommand;
          if (conversionNotice) sayDependencyShift({ notify: conversionNotice.notify, pageId, count: receipt.dependencyShifts, tasks: receipt.dependencyShiftTasks, denied: receipt.dependencyShiftsDenied,
            step: command ? { key: `${pageId}/${command}`, shown: () => isCurrentPage(pageId, generation), back: async () => {
              if (await coordinator.commit(pageId, [{ kind: "restoreDateCommand", dateRestore: { mutationId: command, forward: false } }])) await reread(pageId);
            } } : undefined });
        } else if ((receipt.relatedChanges || receipt.counts.dependencyEdges > 0
          // A property deleted takes the other of a dependency pair with it.
          || operations.some((operation) => operation.kind === "deleteProperty"))
          && isCurrentPage(pageId, generation)) void reread(pageId);
        if (!receipt.dependencyShifts && receipt.dependencyShiftsDenied && isCurrentPage(pageId, generation) && conversionNotice) {
          sayDependencyShift({ notify: conversionNotice.notify, pageId, count: 0, tasks: receipt.dependencyShiftTasks, denied: receipt.dependencyShiftsDenied });
        }
        recordPropertySteps(pageId, operations, generation, String(receipt.graphRevision));
        // A property type change converted cells on the server; reread them
        // and offer to undo the change. A relation may target another page's
        // database, which the page graph shows only once reread.
        const conversions = receipt.propertyConversions ?? [];
        const changedProperties = operations.flatMap((operation) => (
          operation.kind === "upsertProperty" ? [operation.property.id] : []
        ));
        const remakeKey = changedProperties.length ? conversionKey(pageId, changedProperties) : "";
        const remadeWithout = remade.get(remakeKey);
        if (remadeWithout && !conversions.length) {
          remade.delete(remakeKey);
          remadeWithout([]);
        }
        const relationTargeted = operations.some((operation) => (
          operation.kind === "upsertProperty" && operation.property.type === "relation"
        ));
        // Its cells convert on the page holding it, a linked database's source; the page shown is read again.
        const stepPage = stepPageOf(pageId, generation);
        if ((conversions.length || relationTargeted) && stepPage) {
          // The complete graph, so rows do not blink out while later windows
          // load, read once the writes made meanwhile are saved (rereadGraph):
          // read at once, it was the page from before the view's column for a
          // new relation, which the view then showed, and the next column
          // added was refused as a view changed elsewhere (2026-11-12).
          const reread = () => rereadGraph(stepPage);
          // The change is the page's next ⌘Z from now, before its cells are
          // read: pressed right away it would undo what came before.
          let stepsReady: (steps: { back(): Promise<boolean>; again(): Promise<boolean> } | null) => void = () => undefined;
          const steps = new Promise<{ back(): Promise<boolean>; again(): Promise<boolean> } | null>((done) => { stepsReady = done; });
          if (conversions.length && !remade.has(remakeKey)) {
            onPropertyConversion?.(`conversion/${conversions.join(",")}/${receipt.graphRevision}`, async (forward) => {
              const ready = await steps;
              return ready ? (forward ? ready.again() : ready.back()) : true;
            });
          }
          void reread().then(async () => {
            if (!conversions.length || !isCurrentPage(stepPage, generation)) return;
            const latest = await Promise.all(conversions.map((propertyId) => (
              api.readDatabasePropertyConversion(session, pageId, propertyId)
            )));
            const restorable = latest.flatMap(({ conversion }) => (
              conversion?.status === "completed" ? [conversion] : []
            ));
            const waiting = remade.get(remakeKey);
            if (waiting) {
              remade.delete(remakeKey);
              waiting(restorable);
              return;
            }
            if (!restorable.length) return;
            // ⌘Z puts the cells kept from before the change back, as Notion's
            // (seen 2026-11-02), and ⌘⇧Z makes the change again; the notice's
            // undo is the same step.
            const changes = operations.flatMap((operation) => (
              operation.kind === "upsertProperty" && conversions.includes(operation.property.id) ? [operation] : []
            ));
            const remakeAs = conversionKey(pageId, changes.map(({ property }) => property.id));
            let kept = restorable;
            let changed = true;
            // The conversions a change made again makes, until they are read.
            let remaking: Promise<void> | null = null;
            const back = async () => {
              if (remaking) await Promise.race([remaking, new Promise((done) => window.setTimeout(done, 60_000))]);
              if (!changed) return true;
              for (const conversion of kept) {
                await api.restoreDatabasePropertyConversion(session, pageId, conversion.propertyId, conversion.id);
              }
              await api.drainDatabasePropertyConversions(session, pageId, kept.map(({ propertyId }) => propertyId));
              changed = false;
              if (isCurrentPage(stepPage, generation)) await reread();
              return true;
            };
            const again = async () => {
              if (changed) return true;
              remaking = new Promise<void>((done) => remade.set(remakeAs, (next) => {
                kept = next;
                changed = next.length > 0;
                remaking = null;
                done();
              }));
              const saved = await coordinator.commit(pageId, graphViewUpdates(graphRef.current, changes));
              if (!saved) {
                remade.delete(remakeAs);
                remaking = null;
              }
              return saved;
            };
            stepsReady({ back, again });
            if (!conversionNotice) return;
            const { labels, notify } = conversionNotice;
            notify(labels.changed, "default", {
              label: labels.undo,
              onClick: async () => {
                try {
                  await back();
                  notify(labels.restored);
                } catch {
                  notify(labels.restoreFailed, "error");
                }
              },
            });
          }).finally(() => stepsReady(null));
        }
      },
      keep: mutationOutbox ? (pageId, mutationId, operations, text) => (
        mutationOutbox.keepGraph({ mutationId, operations, pageId, ...text }).catch(() => undefined)
      ) : undefined,
      // A change is kept in the outbox first and goes out after every older
      // one; when the server can't be reached it waits there, on screen.
      write: (pageId, mutationId, operations, signal, text) => sendContentGraphChange(session, pageId, operations, { mutationId, signal, ...text }),
    });
    // A kept change the server refused once it could be reached: say so (a lock's
    // refusal with what it let go: useLockRefusedKeptNotice) and show what the server holds; one of
    // a page not open is said too, as the outbox dropped it (2026-11-13: one whose page was gone stayed silent).
    const stopLane = mutationOutbox?.subscribeGraphLane((event) => {
      if (event.kind === "merged" && event.pageId === scopeRef.current.pageId) {
        void reread(event.pageId);
        return;
      }
      if (event.kind !== "rejected") return;
      // A page's database start is said once, as that, by its sender where one is left (databaseStartRefusal.ts).
      const start = event.settings ? null : keptDatabaseStartRefusal(event.pageId, event.written);
      if (start) setMessage(start, "error");
      if (event.pageId !== scopeRef.current.pageId) {
        if (start === null) setMessage(keptChangeRefusedMessage(event.error), "error");
        return;
      }
      if (start === null && !productLockRefusalReason(event.error)) sayGraphChangeRefusal(setMessage, event.error, [], false, event.pageId);
      void reread(event.pageId);
    });
    // After the server changes a page on its own, publish the page's complete
    // graph so the editor shows what the server now holds, once it holds the
    // reader's own changes too.
    const rereadGraph = async (pageId: string) => {
      const generation = scopeRef.current.generation;
      const refreshed = await coordinator.readSettled(pageId, () => collectGraph(
        session, pageId, null, () => isCurrentPage(pageId, generation),
      ));
      if (!refreshed || !isCurrentPage(pageId, generation)) return null;
      // Copies drawn here that the server is still making stay (copiesOnTheirWay.ts).
      setGraph(withCopiesOnTheirWay(refreshed.graph));
      graphRevision.current = refreshed.snapshotRevision;
      onAccess(refreshed.canEdit !== false, refreshed.canSuggest === true);
      return refreshed.graph;
    };
    const reread = async (pageId: string) => (await rereadGraph(pageId)) !== null;
    // The page's changes after the position it shows, read as the room tells
    // of them: the rows they alone changed, and its databases when they
    // changed those, when they say so, else the whole graph. The position is
    // the one shown once this tab's own writes settled, kept ones too and
    // those kept before its reload ("own"); another tab's kept in the outbox
    // since come through the room once saved.
    const follow = async (pageId: string) => {
      const generation = scopeRef.current.generation;
      const read = await coordinator.readSettled(pageId, () => collectContentGraphChanges(
        session, pageId, graphRevision.current, () => isCurrentPage(pageId, generation),
      ), "own");
      if (!read || !isCurrentPage(pageId, generation)) return false;
      // Databases it shows now whose rows it doesn't hold come with the whole graph.
      if (read.changes && changesShowOtherSources(graphRef.current, read.changes)) return reread(pageId);
      if (read.changes === null) setGraph(withCopiesOnTheirWay(read.graph));
      else {
        const { changes } = read;
        setGraph((current) => withChangedRows(current, changes));
      }
      graphRevision.current = read.snapshotRevision;
      onAccess(read.canEdit !== false, read.canSuggest === true);
      return true;
    };
    // A write's run sent after another is read against the graph it was made on (`madeOn`, persistGraphOperationRuns),
    // not the screen, which shows the write by then: its views would go as changes from themselves.
    const commit = (pageId: string, operations: GraphOperation[], madeOn?: ContentGraph) => {
      noteBodyEdit(pageId, operations);
      const made = madeOn ?? graphRef.current;
      if (!propertyStepping) for (const operation of operations) {
        if (operation.kind === "upsertProperty" && !made.properties.some((property) => property.id === operation.property.id)) madeProperties.add(operation.property.id);
      }
      const written = coordinator.commit(pageId, graphViewUpdates(made, operations));
      // A property deleted is a step of ⌘Z as it is sent: one pressed at
      // once waits for the deletion to be taken, then brings it back
      // (2026-11-07: pressed before the save ended, it did nothing, or failed
      // on "a newer state" and the step was lost; Notion's comes back at once).
      const generation = scopeRef.current.generation;
      const deleted = propertyStepping ? [] : operations.flatMap((operation) => (
        operation.kind === "deleteProperty" && operation.propertyDeletion ? [operation.propertyDeletion.id] : []
      ));
      const stepPage = deleted.length ? stepPageOf(pageId, generation) : null;
      if (stepPage) {
        for (const id of deleted) deletionsStepped.add(id);
        void written.then((taken) => { if (!taken) for (const id of deleted) deletionsStepped.delete(id); });
        const { deleteProperties, restoreProperties } = propertySteps(stepPage, generation);
        const shown = viewsShownBy(made.views, deleted);
        const related = relatedChoicesOf(operations);
        stepsSent += 1;
        onPropertyConversion?.(`property-delete/${deleted.join(",")}/sent-${stepsSent}`, (forward) => steppingProperties(async () => (
          forward ? deleteProperties(deleted, related, pageId) : (await written) ? restoreProperties(deleted, shown, pageId) : true)));
      }
      return written;
    };
    // A person's change to a database's views — a view made, with the
    // properties made for it, or a view's settings — is one step of the
    // page's ⌘Z once taken (lib/graphViewSteps): its properties made are no
    // step of their own, and a change a program makes for them (a column
    // placed for a property just made) is sent with commit instead. One sent
    // page by page is one step once its last run is taken (`change`).
    let viewStepsSent = 0;
    const commitViewStep = (pageId: string, operations: GraphOperation[], madeOn?: ContentGraph, change?: GraphOperationChange) => {
      const generation = scopeRef.current.generation;
      const whole = change ?? { graph: madeOn ?? graphRef.current, pageId, operations };
      const step = propertyStepping || change?.last === false ? null : graphViewStepOf(whole.graph, whole.operations);
      propertyStepping += 1;
      let written: Promise<boolean>;
      try { written = commit(pageId, operations, madeOn); } finally { propertyStepping -= 1; }
      const pages = step && graphViewStepPages(whole.graph, whole.pageId ?? pageId, whole.operations, step);
      if (!step || !pages || !isCurrentPage(pages.views, generation)) return written;
      viewStepsSent += 1;
      const key = `view-step/${step.views.map(({ after }) => after.id).join(",")}/${viewStepsSent}`;
      void written.then((saved) => {
        if (!saved || !isCurrentPage(pages.views, generation)) return;
        const { deleteProperties } = propertySteps(pages.views, generation);
        const taking = graphViewStepCommand(step, whole.operations, pages, {
          graph: () => graphRef.current, setGraph,
          restoreProperty: (holder, id) => restoreDatabaseProperty(session, holder, id),
          commitViews: (writes) => coordinator.commit(pages.views, writes),
          deleteProperties: (ids, holder) => deleteProperties(ids, {}, holder),
          reread: async () => { if (isCurrentPage(pages.views, generation)) await rereadGraph(pages.views); },
        });
        onPropertyConversion?.(key, (forward) => steppingProperties(() => taking(forward)));
      });
      return written;
    };
    return {
      ...coordinator,
      dispose: () => {
        stopLane?.();
        coordinator.dispose();
      },
      commit,
      commitViewStep,
      // The writes of a step being taken back or made again, which make no step of their own (sourceLinkStep.ts).
      commitStepping: (pageId: string, operations: GraphOperation[]) => steppingProperties(() => commit(pageId, operations)),
      queue: (pageId: string, operations: GraphOperation[], soon?: boolean) => {
        noteBodyEdit(pageId, operations);
        return coordinator.queue(pageId, graphViewUpdates(graphRef.current, operations), soon);
      },
      reread,
      follow,
    };
  }, [conversionNotice, graphRevision, mutationOutbox, onAccess, onDateCommand, onPropertyConversion, session, setGraph, setMessage, settleToolbarDataSources]);
}
