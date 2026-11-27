"use client";

import { useCallback, useEffect, useId, useMemo, useState, type ReactNode } from "react";
import { api } from "@/api";
import { onCommentRevision } from "@/lib/commentRevisionEvents";
import type { TextSpan } from "@/lib/types";
import type { Session } from "@/types";
import { commentPropertyId, quoteRange } from "../comments/commentModel";
import { keepCommentDraft, startNewComment } from "../comments/keptDraftQuestion";
import { marginShowsThread, openComments } from "../comments/commentPanelEvents";
import { usePageCommentSummary } from "../page/usePageCommentSummary";
import { CellCommentsContext, type CellComments } from "./cellCommentsContext";
import { CellCommentMarks } from "./CellCommentMarks";
import { RowPropertyCommentsPopover } from "./RowPropertyComments";

type CellProperty = { id: string; name: string; type?: string; config?: unknown };

type NewThread = { kind: "new"; rowId: string; property: CellProperty; quote: string; range: { start: number; end: number }; rect: DOMRect;
  /** Its draft put away before, back in its field. */
  draft?: readonly TextSpan[] };

type Active =
  | NewThread
  | { kind: "property"; rowId: string; property: CellProperty; quote: string; rect: DOMRect }
  | { kind: "thread"; rowId: string; property: CellProperty; commentId: string; rect: DOMRect }
  | { kind: "row"; rowId: string; rect: DOMRect };

/**
 * The cell comments a database view's cells and row page share
 * (cellCommentsContext.ts). A thread on some of a value's words is marked
 * there by the server, as no edit of the row (comment_cell_anchors.go); the
 * cells show the open threads' marks alone (CellCommentMarks).
 */
export function CellCommentsProvider({ session, containerPageId, workspaceId, canEditPage, canComment, rowId, children }: {
  session: Session | null;
  containerPageId: string;
  workspaceId?: string;
  canEditPage: boolean;
  /** Whether the reader may start or answer discussions here. */
  canComment: boolean;
  /**
   * One row's page opened on its own, whose open discussions are read from
   * the row's own, as its reader may not open the page holding it.
   */
  rowId?: string;
  children: ReactNode;
}) {
  const [active, setActive] = useState<Active | null>(null);
  // A new thread's draft put away by Escape, as Notion's keeps one (seen
  // 2026-11-12): starting another asks first whether to go back to it or
  // delete it; deleted, nothing new opens.
  // Its words stay marked, fainter, until it is taken back up or deleted.
  // It is the one draft a new comment anywhere asks about (keptDraftQuestion.tsx).
  const owner = useId();
  const [keptAt, setKeptAt] = useState<NewThread | null>(null);
  const keep = useCallback((next: { at: NewThread; draft: TextSpan[] } | null) => {
    setKeptAt(next?.at ?? null);
    keepCommentDraft(owner, next ? {
      back: () => { keep(null); setActive({ ...next.at, draft: next.draft }); },
      drop: () => keep(null),
    } : null);
  }, [owner]);
  useEffect(() => () => keepCommentDraft(owner, null), [owner]);
  // The words of the comment last sent from here stay marked until the
  // value they are in brings the thread's own mark.
  const [sentAt, setSentAt] = useState<NewThread | null>(null);
  const begin = useCallback((next: Active) => {
    setSentAt(null);
    startNewComment(() => setActive(next));
  }, []);
  // The open value discussions of the page's rows, read at once; again when
  // the page's comments move on and when a discussion here closes.
  const [marks, setMarks] = useState<ReadonlyMap<string, { count: number; commentId: string }>>(new Map());
  const [rowCounts, setRowCounts] = useState<ReadonlyMap<string, number>>(new Map());
  // The open threads on some of a value's words, whose marks show.
  const [wordThreads, setWordThreads] = useState<readonly string[]>([]);
  const [marksTurn, setMarksTurn] = useState(0);
  const own = useRowDiscussionMarks(session, rowId);
  useEffect(() => {
    if (!session || !containerPageId || rowId) return;
    const controller = new AbortController();
    void api.listPropertyDiscussions(session, containerPageId, controller.signal).then((result) => {
      setMarks(new Map(result.marks.map((mark) => [`${mark.rowId}:${mark.propertyId}`, { count: mark.count, commentId: mark.commentId }])));
      setRowCounts(new Map((result.rows ?? []).map(({ rowId, count }) => [rowId, count])));
      setWordThreads(result.threads ?? []);
    }, () => undefined);
    return () => controller.abort();
  }, [containerPageId, marksTurn, rowId, session]);
  useEffect(() => onCommentRevision((change) => {
    if (change.pageId === containerPageId) setMarksTurn((turn) => turn + 1);
  }), [containerPageId]);
  const shownMarks = own?.marks ?? marks;
  const shownWordThreads = own?.wordThreads ?? wordThreads;
  // The words a new comment is being written on, or its kept draft's.
  const writing = active?.kind === "new" ? active : null;
  const pendingAt = writing ?? keptAt ?? sentAt;
  const value = useMemo<CellComments | null>(() => session ? {
    start: (at) => begin({ kind: "new", rowId: at.rowId, property: at.property, quote: at.quote, range: { start: at.start, end: at.end }, rect: at.rect }),
    property: (at) => begin({ kind: "property", ...at }),
    active: active?.kind === "property" ? { rowId: active.rowId, propertyId: active.property.id } : null,
    pending: pendingAt ? { rowId: pendingAt.rowId, propertyId: pendingAt.property.id, start: pendingAt.range.start, end: pendingAt.range.end,
      kept: !writing && pendingAt === keptAt, sent: pendingAt === sentAt } : null,
    // A resolved or deleted thread's words keep its mark unseen: pressed, they
    // open the open thread they share, or nothing.
    // On a wide database page the thread's card beside it is marked instead
    // of a window opening under the words (Notion's, seen 2026-11-12).
    open: ({ commentIds, ...at }) => {
      const commentId = commentIds.find((id) => shownWordThreads.includes(id));
      if (commentId && marginShowsThread(commentId)) openComments({ pageId: at.rowId, threadIds: [commentId], pressedOnly: true });
      else if (commentId) setActive({ kind: "thread", ...at, commentId });
    },
    discussed: (rowId, propertyId) => shownMarks.get(`${rowId}:${propertyId}`),
    rowDiscussions: (rowId) => rowCounts.get(rowId) ?? 0,
    openRow: (at) => setActive({ kind: "row", ...at }),
    canComment,
  } : null, [active, begin, canComment, keptAt, pendingAt, rowCounts, sentAt, session, shownMarks, shownWordThreads, writing]);
  return (
    <CellCommentsContext.Provider value={value}>
      <CellCommentMarks threadIds={shownWordThreads} />
      {children}
      {active && session ? (
        <RowPropertyCommentsPopover key={active.kind === "new" ? `${active.rowId}:${active.range.start}`
          : active.kind === "property" ? `${active.rowId}:${active.property.id}` : active.kind === "row" ? active.rowId : active.commentId}
          comments={{ containerPageId, rowId: active.rowId, session, workspaceId, canEditPage, canComment, openByProperty: new Map() }}
          property={active.kind === "row" ? undefined : active.property} quote={active.kind === "thread" || active.kind === "row" ? "" : active.quote}
          anchor={{ left: active.rect.left + active.rect.width / 2, top: active.rect.bottom }} centered
          range={active.kind === "new" ? active.range : undefined} threadId={active.kind === "thread" ? active.commentId : undefined}
          fromWords={active.kind === "thread"}
          initialDraft={active.kind === "new" ? active.draft : undefined}
          onSent={active.kind === "new" ? () => setSentAt({ ...active, draft: undefined }) : undefined}
          onKeepDraft={active.kind === "new" ? (draft) => {
            keep({ at: { ...active, draft: undefined }, draft });
            setActive(null);
          } : undefined}
          onClose={() => { setActive(null); setMarksTurn((turn) => turn + 1); }} />
      ) : null}
    </CellCommentsContext.Provider>
  );
}

/**
 * One row's open value discussions from its own: the first open one on each
 * whole value, which pressing the value opens, and those on some of a text's
 * words, whose marks show.
 */
function useRowDiscussionMarks(session: Session | null, rowId: string | undefined) {
  const summary = usePageCommentSummary({ pageId: rowId, row: true, session: session ?? undefined });
  return useMemo(() => {
    if (!rowId) return null;
    const marks = new Map<string, { count: number; commentId: string }>();
    const wordThreads: string[] = [];
    const open = summary.threads.filter((thread) => commentPropertyId(thread.body) && !thread.resolved)
      .sort((left, right) => (left.createdAt ?? "").localeCompare(right.createdAt ?? ""));
    for (const thread of open) {
      if (quoteRange(thread.body)) {
        wordThreads.push(thread.id);
        continue;
      }
      const key = `${rowId}:${commentPropertyId(thread.body)}`;
      const mark = marks.get(key);
      marks.set(key, { count: (mark?.count ?? 0) + 1, commentId: mark?.commentId ?? thread.id });
    }
    return { marks: marks as ReadonlyMap<string, { count: number; commentId: string }>, wordThreads: wordThreads as readonly string[] };
  }, [rowId, summary.threads]);
}
