import { useMemo, useRef, type Dispatch, type SetStateAction } from "react";
import type { ContentGraph, GraphProperty } from "@/contentGraph";
import { createNativeBoardProductAdapter, type NativeBoardProductAdapter } from "@/lib/nativeBoardProductAdapter";
import { createNativeDatabaseViewProductAdapter } from "@/lib/nativeDatabaseViewProductAdapter";
import { createNativeFormProductAdapter } from "@/lib/nativeFormProductAdapter";
import { createNativeTableProductAdapter, type NativeTableProductAdapter, type ValueChange } from "@/lib/nativeTableProductAdapter";
import { createNativeTimelineProductAdapter } from "@/lib/nativeTimelineProductAdapter";
import type { RecordRows } from "@/lib/nativeTableRowSteps";
import { TYPED_VALUE_MS, typingRun, valueStepRecorder } from "@/lib/valueSteps";
import type { ProductStatusSink } from "@/lib/productStatus";
import type { Session } from "@/types";
import type { PageGraphStore } from "@/lib/pageGraphStore";
import type { useContentGraphWrites } from "./useContentGraphWrites";
import type { useDatabaseDateCommandHistory } from "./useDatabaseDateCommandHistory";

/**
 * A page body's database adapters: its tables', boards', timelines', forms'
 * and views' writes. They are the same objects while what they write
 * through stays the same, so the databases drawn with them aren't drawn
 * again when the page is for another reason (2026-11-11: a page list
 * refresh, a status line, drew a table of 100,000 rows again, 0.25-0.3
 * seconds each time), nor when the graph changes: each write works on the
 * graph as drawn when it is made (madeAtEachCall), the form's on the page's
 * properties.
 */
export function usePageDatabaseAdapters(input: {
  /** The graph as the page around the body reads it: its properties are current. */
  graph: ContentGraph;
  /** The graph as drawn, which each write is made against. */
  readGraph: () => ContentGraph;
  pageId: string | null;
  graphWrites: ReturnType<typeof useContentGraphWrites>;
  session: Session;
  organizationId?: string;
  workspaceId: string | null;
  setGraph: Dispatch<SetStateAction<ContentGraph>>;
  onStatus: ProductStatusSink;
  dateCommands: ReturnType<typeof useDatabaseDateCommandHistory>;
  /** The open page's graph store, where a ⌘Z's rows show ahead of the server (nativeTableRowSteps.ts). */
  graphStore?: PageGraphStore;
}) {
  const { graph, graphStore, graphWrites, organizationId, pageId, readGraph, session, setGraph, workspaceId } = input;
  // What the adapters tell and step through is the latest the page has.
  const latest = useRef(input);
  latest.current = input;
  // A table's change of values, a row made and rows trashed are steps of the
  // page's ⌘Z, taken by the table as it is then.
  const latestTable = useRef<NativeTableProductAdapter | null>(null);
  const latestBoard = useRef<NativeBoardProductAdapter | null>(null);
  // A cell saved again as it is typed in is one step (lib/valueSteps.ts),
  // which lands at once as it is taken back or written again: its values show then.
  const recordValueSteps = useMemo(() => valueStepRecorder<ValueChange>({
    cell: (change) => `${change.rowId}/${change.propertyId}`,
    record: (key, changes) => void latest.current.dateCommands.recordShownValueCommand(key,
      (forward) => latestTable.current?.restoreValues(changes, forward) ?? Promise.resolve(false)),
    isLatest: (key) => latest.current.dateCommands.isLatestCommand(key),
    drop: (key) => latest.current.dateCommands.dropCommand(key),
  }), []);
  const editedProperties = useRef<{ key: string; ids: string; at: number; run: number; before: readonly GraphProperty[]; after: readonly GraphProperty[] } | null>(null);
  const setMessage: ProductStatusSink = useMemo(() => (message, tone) => latest.current.onStatus(message, tone), []);
  const form = useMemo(() => createNativeFormProductAdapter({
    graph, readGraph, pageId, persistGraphOperations: graphWrites.commit, session, setGraph, setMessage,
  }), [graph.properties, graphWrites, pageId, readGraph, session, setGraph, setMessage]);
  const adapters = useMemo(() => {
    const commands = () => latest.current.dateCommands;
    // Rows made, trashed, copied or taken away, taken by the table as it is
    // then (a board's cards and a timeline's bars too); a row the server refused leaves again.
    // Rows show as their step is taken, which lands at once (recordShownNativeEditorCommand), a press's hold for the
    // server's answer let go first (`letGo`).
    const recordRows: RecordRows = (step) => {
      if (!commands().recordShownValueCommand(step.key, (forward) => {
        step.letGo?.();
        return latestTable.current?.stepRows?.(step, forward) ?? Promise.resolve(false);
      })) return;
      return () => commands().dropCommand(step.key);
    };
    const databaseViewAdapter = madeAtEachCall(() => createNativeDatabaseViewProductAdapter({
      graph: readGraph(), pageId, persistGraphOperations: graphWrites.commit, queueGraphOperations: graphWrites.queue, rereadGraph: graphWrites.reread, session,
      persistViewStep: graphWrites.commitViewStep,
      workspaceId, setGraph, setMessage,
    }));
    const tableAdapter = madeAtEachCall(() => createNativeTableProductAdapter({
      graph: readGraph(), pageId, persistGraphOperations: graphWrites.commit, persistViewStep: graphWrites.commitViewStep, rereadGraph: graphWrites.reread,
      collectGraphOperations: graphWrites.collect, readAfterWrites: graphWrites.readSettled, session, organizationId, workspaceId, setGraph, setMessage, graphStore,
      recordValues: recordValueSteps,
      recordRows,
      recordMove: (step, withDates) => (withDates ? commands().recordMoveWithDates : commands().recordValueCommand)(step.key,
        (forward) => latestTable.current?.stepMove?.(step, forward) ?? Promise.resolve(false)),
      recordProperties: (step) => {
        // Edits to the same properties in a row, as a name typed, are one step, in one run of typing (typingRun).
        const ids = step.key.startsWith("property-edit/") ? step.key.split("/")[1] : "", run = typingRun();
        const last = editedProperties.current;
        if (ids && last?.ids === ids && last.run === run && Date.now() - last.at < TYPED_VALUE_MS && commands().isLatestCommand(last.key)) {
          last.after = step.after;
          last.at = Date.now();
          return;
        }
        const kept = { key: step.key, ids, at: Date.now(), run, before: step.before, after: step.after };
        editedProperties.current = ids ? kept : null;
        commands().recordValueCommand(step.key,
          (forward) => latestTable.current?.stepProperties?.(forward ? kept.after : kept.before) ?? Promise.resolve(false));
      },
    }));
    const boardAdapter = madeAtEachCall(() => createNativeBoardProductAdapter({
      graph: readGraph(), pageId, persistGraphOperations: graphWrites.commit, persistViewStep: graphWrites.commitViewStep, setGraph, setMessage, editedBy: session?.user.id,
      recordMove: (step) => commands().recordValueCommand(step.key, (forward) => latestBoard.current?.stepMove(step, forward) ?? Promise.resolve(false)),
      recordRows,
    }));
    const timelineAdapter = madeAtEachCall(() => createNativeTimelineProductAdapter({
      graph: readGraph(), pageId, persistGraphOperations: graphWrites.commit, persistViewStep: graphWrites.commitViewStep, session, setGraph, setMessage,
      recordRows, recordValues: recordValueSteps,
    }));
    return { databaseViewAdapter, tableAdapter, boardAdapter, timelineAdapter };
  }, [graphStore, graphWrites, organizationId, pageId, readGraph, recordValueSteps, session, setGraph, setMessage, workspaceId]);
  latestTable.current = adapters.tableAdapter;
  latestBoard.current = adapters.boardAdapter;
  return useMemo(() => ({ ...form, ...adapters }), [adapters, form]);
}

/**
 * What `make` makes, each of its functions (its objects' too) run on what
 * `make` makes when it is called: an adapter made over the graph, that works
 * on the graph as it is then, as the adapter made at the last change would.
 */
function madeAtEachCall<T extends object>(make: () => T): T {
  const made = make() as Record<string, unknown>;
  return Object.fromEntries(Object.entries(made).map(([key, value]) => [key,
    typeof value === "function" ? (...args: unknown[]) => (make() as Record<string, (...args: unknown[]) => unknown>)[key](...args)
      : value && typeof value === "object" && !Array.isArray(value) ? madeAtEachCall(() => (make() as Record<string, object>)[key])
        : value])) as T;
}
