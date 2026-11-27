import { compareGraphOrder, graphViewUpdates, type ContentGraph, type GraphOperation, type GraphProperty, type GraphView } from "@/contentGraph";
import { leftLinkedView } from "@/lib/databaseStarterLinked";
import { graphOperationRuns } from "@/lib/nativeGraphAuthority";

/**
 * A person's change to a database's views as one step of the page's ⌘Z:
 * each view as it was (none for a view it makes) and as the change left it,
 * and the properties made with them (a new board's "상태"), which go and
 * come back with the step, as Notion's (seen 2026-11-12: a board added went
 * with its "상태" at one ⌘Z; a column hidden came back at the next).
 */
export type GraphViewStep = {
  views: { before?: GraphView; after: GraphView }[];
  madeProperties: string[];
};

/**
 * The step a write to views makes on the graph as drawn, or none when it
 * changes nothing. A linked view waiting in its "새 보기" counts as the view
 * it stays when left (leftLinkedView), as Notion's (seen 2026-11-18, two
 * tabs): left, or made the layout it waited as, it is no step, ⌘Z taking
 * the source's link away (sourceLinkStep.ts); a view of the source chosen
 * there is a step of its own, ⌘Z bringing back the table it waited as.
 */
export function graphViewStepOf(graph: ContentGraph, operations: readonly GraphOperation[]): GraphViewStep | null {
  const views = operations.flatMap((operation) => {
    if (operation.kind !== "upsertView") return [];
    const before = graph.views.find((view) => view.id === operation.view.id);
    return [{ before: before?.config.linkedStarterPending === true ? leftLinkedView(before) : before, after: operation.view }];
  });
  const madeProperties = operations.flatMap((operation) => operation.kind === "upsertProperty"
    && !graph.properties.some((property) => property.id === operation.property.id) ? [operation.property.id] : []);
  const changed = views.some(({ before, after }) => !before || JSON.stringify(before) !== JSON.stringify(after));
  return changed ? { views, madeProperties } : null;
}

/**
 * The pages a step's writes went to (graphOperationRuns), which taking it
 * back or making it again writes: its views' page, and each property it made
 * by the page that holds it — a linked database's on its source's page.
 */
export function graphViewStepPages(
  graph: ContentGraph,
  pageId: string,
  operations: readonly GraphOperation[],
  step: GraphViewStep,
) {
  const runs = graphOperationRuns(graph, pageId, operations) ?? [];
  const pageOf = (holds: (operation: GraphOperation) => boolean) => runs.find((run) => run.operations.some(holds))?.pageId ?? pageId;
  const made = new Map<string, string[]>();
  for (const id of step.madeProperties) {
    const holder = pageOf((operation) => operation.kind === "upsertProperty" && operation.property.id === id);
    made.set(holder, [...made.get(holder) ?? [], id]);
  }
  return { views: pageOf((operation) => operation.kind === "upsertView"), madeProperties: [...made] };
}

/**
 * Taking the step back (forward false) or making it again, on the pages its
 * writes went to (graphViewStepPages). Both ways it is drawn at the press
 * (graphAfterViewStep); taken back, its views are written, then the properties
 * it made deleted; made again, those properties come back on the server
 * before the views naming them are written, and the screen goes back when
 * the server refuses them. Read again after, for their values and a name the
 * server told apart.
 */
export function graphViewStepCommand(
  step: GraphViewStep,
  operations: readonly GraphOperation[],
  pages: ReturnType<typeof graphViewStepPages>,
  page: {
    graph: () => ContentGraph;
    setGraph: (next: (current: ContentGraph) => ContentGraph) => void;
    restoreProperty: (holder: string, id: string) => Promise<unknown>;
    commitViews: (writes: GraphOperation[]) => Promise<boolean>;
    deleteProperties: (ids: string[], holder: string) => Promise<boolean>;
    reread: () => Promise<void>;
  },
) {
  let taken = step;
  const made = pages.madeProperties.flatMap(([, ids]) => ids);
  // The properties it made as last taken away, which come back so.
  let madeBack = operations.flatMap((operation) => operation.kind === "upsertProperty" && made.includes(operation.property.id) ? [operation.property] : []);
  return async (forward: boolean) => {
    const graph = page.graph();
    taken = graphViewStepAsShown(taken, graph);
    const shown = graph.properties.filter(({ id }) => made.includes(id));
    if (!forward && shown.length) madeBack = shown;
    const writes = graphViewUpdates(graph, graphViewStepWrites(taken, forward));
    page.setGraph((current) => graphAfterViewStep(current, taken, forward, madeBack));
    if (forward) {
      try {
        for (const [holder, ids] of pages.madeProperties) for (const id of ids) await page.restoreProperty(holder, id);
      } catch (error) {
        page.setGraph((current) => graphAfterViewStep(current, taken, false, [], made));
        throw error;
      }
    }
    let done = await page.commitViews(writes);
    for (const [holder, ids] of pages.madeProperties) if (done && !forward) done = await page.deleteProperties(ids, holder);
    if (done && forward && made.length) await page.reread();
    return done;
  };
}

/**
 * The step on the views as the page now shows them, as it is taken back or
 * made again: only what the step changed goes back or comes again, so a
 * change since that is no step of its own stays, as Notion's (seen
 * 2026-11-12: a timeline's table shown, then its zoom changed, ⌘Z hid the
 * table and kept the zoom). A view the step made is taken as shown, a
 * copy's name and settings with it, so ⌘⇧Z brings it back whole.
 */
export function graphViewStepAsShown(step: GraphViewStep, graph: ContentGraph): GraphViewStep {
  return { ...step, views: step.views.map(({ before, after }) => {
    const shown = graph.views.find((view) => view.id === after.id);
    if (!shown) return { before, after };
    if (!before) return { before, after: shown };
    return { before: viewWithChange(shown, after, before), after: viewWithChange(shown, before, after) };
  }) };
}

/** The view as shown, with each of its fields and settings that differ from `from` to `to` as `to` has them. */
function viewWithChange(shown: GraphView, from: GraphView, to: GraphView): GraphView {
  const changed = (base: Record<string, unknown>, a: Record<string, unknown>, b: Record<string, unknown>, skip?: string) => {
    const next = { ...base };
    for (const key of new Set([...Object.keys(a), ...Object.keys(b)])) {
      if (key === skip || JSON.stringify(a[key]) === JSON.stringify(b[key])) continue;
      if (b[key] === undefined) delete next[key];
      else next[key] = b[key];
    }
    return next;
  };
  const fields = changed(shown as unknown as Record<string, unknown>, from as unknown as Record<string, unknown>, to as unknown as Record<string, unknown>, "config");
  return { ...fields, config: changed(shown.config, from.config, to.config) } as unknown as GraphView;
}

/** The writes that take the step back (forward false) or make it again: views it made go, the others are written as they were. */
export function graphViewStepWrites(step: GraphViewStep, forward: boolean): GraphOperation[] {
  return step.views.map(({ before, after }): GraphOperation => forward ? { kind: "upsertView", view: after }
    : before ? { kind: "upsertView", view: before } : { kind: "deleteView", viewDeletion: { id: after.id } });
}

/**
 * The page as the step taken back or made again shows it, drawn at the press
 * before the server has it, as Notion's ⌘⇧Z (seen 2026-11-18: a calendar
 * added with its "날짜", taken back, came back in 39–47 ms, its request
 * answered later): its views, and made again, the properties it made
 * (`madeBack`, as they were taken away), in their places; those named by
 * `gone` leave (a step made again that the server refused, drawn back).
 */
function graphAfterViewStep(
  graph: ContentGraph,
  step: GraphViewStep,
  forward: boolean,
  madeBack: readonly GraphProperty[] = [],
  gone: readonly string[] = [],
): ContentGraph {
  const views = graphViewsAfterStep(graph.views, step, forward);
  const kept = gone.length ? graph.properties.filter(({ id }) => !gone.includes(id)) : graph.properties;
  const back = forward ? madeBack.filter((made) => !kept.some(({ id }) => id === made.id)) : [];
  return { ...graph, views, properties: back.length ? [...kept, ...back].sort(compareGraphOrder) : kept };
}

/** The page's views once the step is taken back or made again. */
function graphViewsAfterStep(views: readonly GraphView[], step: GraphViewStep, forward: boolean): GraphView[] {
  const stepped = new Map(step.views.map(({ before, after }) => [after.id, forward ? after : before]));
  const placed = views.flatMap((view) => {
    if (!stepped.has(view.id)) return [view];
    const next = stepped.get(view.id);
    return next ? [next] : [];
  });
  const back = [...stepped.values()].filter((view): view is GraphView => !!view && !views.some(({ id }) => id === view.id));
  return [...placed, ...back];
}
