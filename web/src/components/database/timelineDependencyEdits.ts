import type { DatabaseDependencyGraphEdge } from "@/databaseQueryWire.generated";
import { newId } from "@/lib/ids";
import type { TimelineViewOperationContext, TimelineViewOperations } from "./TimelineView";

type LinksWindow = { edges: DatabaseDependencyGraphEdge[] };
type Run = <Result>(requestKey: string, perform: (context: TimelineViewOperationContext) => Promise<Result>,
  publish?: (result: Result, context: TimelineViewOperationContext) => void) => Promise<void>;

/**
 * Drawing a timeline's dependency link and taking one away, for one who may
 * change the timeline. A link drawn joins the timeline's links once the
 * server has it, under the ID it was sent with. A link taken away (its
 * line's ✕, TimelineDependencyArrows) leaves them at once, every link
 * between the two rows by its ID, and the server is told after; one it
 * refuses comes back.
 */
export function timelineDependencyEdits<Window extends LinksWindow>({ readOnly, pairs, edges, revision, setWindow, run, operations }: {
  readOnly: boolean;
  /** The links drawn, each [successor, predecessor]. */
  pairs: readonly (readonly [string, string])[];
  edges: readonly DatabaseDependencyGraphEdge[];
  revision: number;
  setWindow(update: (current: Window) => Window): void;
  run: Run;
  operations: Pick<TimelineViewOperations, "createDependency" | "deleteDependency">;
}) {
  const between = (successor: string, predecessor: string) => (edge: DatabaseDependencyGraphEdge) =>
    edge.successorRowId === successor && edge.predecessorRowId === predecessor;
  return {
    create(successor: string, predecessor: string) {
      if (readOnly || pairs.some(([currentSuccessor, currentPredecessor]) => currentSuccessor === successor && currentPredecessor === predecessor)) return;
      const id = newId();
      void run(`dependency:create:${successor}`, (context) => operations.createDependency(successor, predecessor, revision, context, id),
        () => setWindow((current) => ({ ...current, edges: [...current.edges, { id, predecessorRowId: predecessor, successorRowId: successor }] })));
    },
    remove(successor: string, predecessor: string) {
      const deleteDependency = operations.deleteDependency;
      const taken = edges.filter(between(successor, predecessor));
      const ids = taken.flatMap((edge) => edge.id ? [edge.id] : []);
      if (readOnly || !deleteDependency || !ids.length) return;
      setWindow((current) => ({ ...current, edges: current.edges.filter((edge) => !between(successor, predecessor)(edge)) }));
      void run(`dependency:delete:${successor}:${predecessor}`, (context) => deleteDependency(ids, revision, context), (kept) => {
        if (!kept) setWindow((current) => ({ ...current, edges: [...current.edges.filter((edge) => !between(successor, predecessor)(edge)), ...taken] }));
      });
    },
  };
}
