import { useCallback, useMemo, useRef, type Dispatch, type SetStateAction } from "react";
import type { ContentGraph, GraphOperation } from "@/contentGraph";
import { authenticatedProductErrorMessage } from "@/lib/authenticatedProductErrors";
import { contentGraphOutboxOf, sendContentGraphChange } from "@/lib/contentGraphSend";
import { createNativeContentGraphWriteCoordinator } from "@/lib/nativeContentGraphWriteCoordinator";
import { tellPageEditedHere } from "@/lib/pageEditedHere";
import type { BlockChangeText } from "@/lib/blockChangeText";
import { graphWriteEditsPage } from "@/components/product/graphWritePageEdit";
import type { ProductStatusSink } from "@/lib/productStatus";
import { useProductToast } from "@/lib/productToast";
import { useSurfaceUndo } from "@/lib/surfaceUndo";
import type { Session } from "@/types";
import type { useContentGraphWrites } from "@/components/product/useContentGraphWrites";
import { useCommittedResourceDisposal } from "@/components/product/useCommittedResourceDisposal";
import type { useDatabaseDateCommandHistory } from "@/components/product/useDatabaseDateCommandHistory";
import { usePageDatabaseAdapters } from "@/components/product/usePageDatabaseAdapters";

const NO_WRITES = { dispose() {} };

/**
 * The writes of People's database for its owner, as a database page's
 * (usePageDatabaseAdapters): its views, the properties added after its five
 * and their values go to its page as any database's change, shown at once and
 * saved behind it through the same write coordinator, in the order made and
 * a value typed in saved once the typing ends (lib/typedWrites.ts) — one
 * save, one run of its automations — one the server refuses read again from
 * the server and said; each change of values
 * a step of People's ⌘Z (useSurfaceUndo), as Notion's (seen 2026-11-15: a
 * value written in People went with ⌘Z and came back with ⌘⇧Z). Without its
 * owner's page (`input` none) it writes nothing. Of its five its About alone
 * is written, into its person's profile, as every view of People writes it
 * (peopleSourceCells.ts), its steps in the same history.
 */
export function usePeopleDatabaseWrites({ input, graph, setGraph, onReread }: {
  input: { session: Session; workspaceId: string; pageId: string } | null;
  graph: ContentGraph;
  setGraph: Dispatch<SetStateAction<ContentGraph>>;
  onReread(): Promise<boolean>;
}) {
  const { notify } = useProductToast();
  const latest = useRef(graph);
  latest.current = graph;
  const readGraph = useCallback(() => latest.current, []);
  const history = useSurfaceUndo(input ? `people:${input.workspaceId}` : "people");
  const session = input?.session;
  const reread = useRef(onReread);
  reread.current = onReread;
  const said = useRef(notify);
  said.current = notify;
  // Its owner's writes, as a database page's (useContentGraphWrites): kept in
  // the account's outbox first and sent after every older one.
  const coordinator = useMemo(() => {
    if (!session) return null;
    const outbox = contentGraphOutboxOf(session);
    return createNativeContentGraphWriteCoordinator({
      onFailure: async (error) => {
        said.current(authenticatedProductErrorMessage(error, "contentGraphUpdate"), "error");
        await reread.current();
      },
      onQueued: () => {},
      onSaved: () => {},
      ...(outbox ? {
        pagesWaiting: outbox.graphPagesWaiting,
        keep: (pageId: string, mutationId: string, operations: GraphOperation[], text?: BlockChangeText) => {
          void outbox.keepGraph({ mutationId, operations, pageId, ...text }).catch(() => undefined);
        },
      } : {}),
      write: (pageId, mutationId, operations, signal, text) => sendContentGraphChange(session, pageId, operations, { mutationId, signal, ...text }),
    });
  }, [session]);
  useCommittedResourceDisposal(coordinator ?? NO_WRITES);
  // A write that edits People — a property, a view, a row added
  // (graphWritePageEdit.ts) — shows it last edited now, by its owner, as a
  // database page's does (usePageBodyEditing).
  const editedBy = session?.user.id;
  const told = useCallback((pageId: string, operations: readonly GraphOperation[]) => {
    if (!editedBy || !graphWriteEditsPage(latest.current, operations, true)) return;
    tellPageEditedHere({ id: pageId, updatedAt: new Date().toISOString(), lastEditedBy: editedBy });
  }, [editedBy]);
  const graphWrites = useMemo(() => ({
    commit: async (pageId: string, operations: GraphOperation[]) => { told(pageId, operations); return coordinator?.commit(pageId, operations) ?? false; },
    commitViewStep: async (pageId: string, operations: GraphOperation[]) => { told(pageId, operations); return coordinator?.commit(pageId, operations) ?? false; },
    collect: async (pageId: string, operations: GraphOperation[]) => { told(pageId, operations); return coordinator?.collect(pageId, operations) ?? false; },
    queue: undefined,
    reread: async () => reread.current(),
    readSettled: async <Value,>(pageId: string, read: () => Promise<Value | null>) => coordinator ? coordinator.readSettled(pageId, read) : read(),
  }) as unknown as ReturnType<typeof useContentGraphWrites>, [coordinator, told]);
  const dateCommands = useMemo(() => ({
    recordValueCommand: (key: string, step: (forward: boolean) => Promise<boolean>) => {
      history({ key, back: () => void step(false), again: () => void step(true) });
      return true;
    },
    // The screen's own steps land as they are taken already (lib/surfaceUndo.ts).
    recordShownValueCommand: (key: string, step: (forward: boolean) => Promise<boolean>) => {
      history({ key, back: () => void step(false), again: () => void step(true) });
      return true;
    },
    recordDateCommand: (key: string, step: (forward: boolean) => Promise<boolean>) => {
      history({ key, back: () => void step(false), again: () => void step(true) });
      return true;
    },
    // People's rows have no dates a move carries: a move is its own step.
    recordMoveWithDates: (key: string, step: (forward: boolean) => Promise<boolean>) => {
      history({ key, back: () => void step(false), again: () => void step(true) });
      return true;
    },
    onDateCommand: () => {},
    isLatestCommand: (key: string) => history.isLatest(key),
    dropCommand: (key: string) => history.drop(key),
  }) as ReturnType<typeof useDatabaseDateCommandHistory>, [history]);
  const onStatus = useCallback<ProductStatusSink>((message, tone) => {
    if (tone === "error") notify(message, "error");
  }, [notify]);
  const adapters = usePageDatabaseAdapters({
    graph, readGraph, pageId: input?.pageId ?? null, graphWrites, session: session as Session, workspaceId: input?.workspaceId ?? null,
    setGraph, onStatus, dateCommands,
  });
  if (!input) return {};
  return {
    tableAdapter: adapters.tableAdapter, databaseViewAdapter: adapters.databaseViewAdapter,
    boardAdapter: adapters.boardAdapter, timelineAdapter: adapters.timelineAdapter, addGraphProperty: adapters.addGraphProperty,
  };
}
