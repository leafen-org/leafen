import type { NativeGraphMutationOutbox } from "./nativePageMutationBrowser";

/**
 * Settles once the outbox sent a change it kept (true), or the server
 * refused it (false): its answer, the lane drained, or its page no longer
 * waiting — another tab sent it, or it went before this was asked.
 */
export function whenKeptChangeWent(outbox: Pick<NativeGraphMutationOutbox, "graphPagesWaiting" | "subscribeGraphLane">,
  pageId: string, mutationId: string): Promise<boolean> {
  return new Promise((settle) => {
    let finished = false, stopLane = () => {}, stopWaiting = () => {};
    const done = (went: boolean) => {
      if (finished) return;
      finished = true;
      stopLane();
      stopWaiting();
      settle(went);
    };
    stopLane = outbox.subscribeGraphLane((event) => {
      if (event.kind === "answered" && event.mutationId === mutationId) done(event.refused !== true);
      else if (event.kind === "drained") done(true);
    });
    stopWaiting = outbox.graphPagesWaiting.subscribe(() => { if (!outbox.graphPagesWaiting.has(pageId)) done(true); });
    if (!outbox.graphPagesWaiting.has(pageId)) done(true);
    if (finished) { stopLane(); stopWaiting(); }
  });
}
