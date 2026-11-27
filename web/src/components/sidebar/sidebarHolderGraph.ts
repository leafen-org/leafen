import { useEffect, useState } from "react";
import type { ContentGraph, GraphOperation } from "@/contentGraph";
import { overlayPendingGraphOperations } from "@/components/product/pendingGraphOverlay";
import { useProductPages } from "@/components/product/productPagesContext";
import { collectAuthenticatedContentGraph } from "@/lib/authenticatedContentGraph";
import { contentGraphOutboxOf } from "@/lib/contentGraphSend";
import { isServerUnreachable, readUntilServerAnswers } from "@/lib/serverReach";
import { afterOwnWrites } from "@/lib/ownWritesInFlight";
import { drawGraphBeside, onGraphChanged } from "@/lib/sidebarGraphFollow";
import type { Session } from "@/types";

export type HolderGraph = { status: "loading" } | { status: "missing" } | { status: "ready"; graph: ContentGraph; canEdit: boolean };

/**
 * The content of the page holding a database, every window of it in one
 * snapshot as the page itself is read — a section's rows and "모두 보기"
 * are the view's, all of them — again when the revision changes, and not at
 * all until enabled; and again when it changed elsewhere — another screen
 * or device, the API, an agent — as the change socket hears it
 * (lib/sidebarGraphFollow.ts), after this tab's own changes on their way
 * have landed, once for the words heard while a read was under way. The
 * changes to it that wait here for the server show with it: with the server
 * out of reach, a reread keeps what was read and shows them on it, and it is
 * read again once they are sent, or once the server may answer
 * (readUntilServerAnswers) — read first while it didn't, a section stayed
 * without its database's name and rows after the server was back (2026-11-25).
 */
export function useHolderGraph(session: Session, pageId: string, enabled = true, revision: number | string = 0): HolderGraph {
  // What was read is kept apart from what shows, which holds the changes
  // waiting here: a reread the server can't answer shows them on what was
  // read, not again on what showed (made again, a move goes by where the
  // blocks stand after it).
  const [state, setState] = useState<{ key: string; graph: HolderGraph; read?: ContentGraph }>({ key: "", graph: { status: "loading" } });
  const [sent, setSent] = useState(0);
  // The words heard that its graph changed elsewhere, and the last one read for.
  const [word, setWord] = useState(0);
  const [heard, setHeard] = useState(0);
  const key = `${pageId}:${revision}:${sent}:${heard}`;
  const outbox = useProductPages()?.mutationOutbox ?? contentGraphOutboxOf(session);
  useEffect(() => {
    if (!enabled) return undefined;
    const stopDrawn = drawGraphBeside(pageId);
    const stopHeard = onGraphChanged(pageId, () => setWord((value) => value + 1));
    return () => { stopDrawn(); stopHeard(); };
  }, [enabled, pageId]);
  // A read under way isn't called off for a word: the words heard meanwhile are read for once it is back.
  const settled = state.key === key;
  useEffect(() => {
    if (!settled || heard === word) return undefined;
    let live = true;
    void afterOwnWrites().then(() => { if (live) setHeard(word); });
    return () => { live = false; };
  }, [heard, settled, word]);
  useEffect(() => {
    let kept = false;
    return outbox?.subscribeGraphLane((event) => {
      if (event.kind === "kept" && event.pageId === pageId) kept = true;
      if (event.kind !== "drained" || !kept) return;
      kept = false;
      setSent((value) => value + 1);
    });
  }, [outbox, pageId]);
  useEffect(() => {
    if (!enabled) return undefined;
    let current = true;
    // Change by change, each placing its blocks as the server will (pendingGraphOverlay.ts).
    const waiting = () => outbox?.pendingGraphWrites(pageId).then((writes) => writes.map(({ operations }) => operations as GraphOperation[]), () => []) ?? [];
    const stop = readUntilServerAnswers(async () => {
      try {
        const read = await collectAuthenticatedContentGraph(session, pageId, null, () => current);
        if (!current || !read) return;
        const kept = await waiting();
        if (current) setState({ key, read: read.graph, graph: { status: "ready", graph: overlayPendingGraphOperations(read.graph, kept), canEdit: read.canEdit === true } });
      } catch (error) {
        if (!current) return;
        const kept = isServerUnreachable(error) ? await waiting() : [];
        if (current) setState((shown) => shown.graph.status === "ready" && shown.read && isServerUnreachable(error) && shown.key.startsWith(`${pageId}:`)
          ? { key, read: shown.read, graph: { ...shown.graph, graph: overlayPendingGraphOperations(shown.read, kept) } }
          : { key, graph: { status: "missing" } });
        // Unanswered, it is read again once the server may answer.
        if (isServerUnreachable(error)) throw error;
      }
    }, () => undefined);
    return () => {
      current = false;
      stop();
    };
  }, [enabled, key, outbox, pageId, session]);
  // A reread keeps showing what was read until it comes back.
  return state.key === key || state.key.startsWith(`${pageId}:`) ? state.graph : { status: "loading" };
}
