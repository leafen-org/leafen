"use client";

import { createContext } from "react";

/** A value's property: its type and settings give its icon where a thread quotes the value. */
type CellProperty = { id: string; name: string; type?: string; config?: unknown };

/**
 * A database cell's comments, as Notion's (seen 2026-10-31): "댓글" in a
 * text cell's selection bar opens a field under the chosen words that
 * starts a thread on the property quoting them; the server marks the words
 * with the thread in the cell's value (comment_cell_anchors.go), which show
 * marked while the thread is open (CellCommentMarks), and pressing them opens
 * that thread under them. The row page's property discussions mark them the
 * same way.
 */
export type CellComments = {
  start(at: { rowId: string; property: CellProperty; quote: string; start: number; end: number; rect: DOMRect }): void;
  /** A cell's "댓글" under the pointer: a discussion on the whole value, as Notion's (seen 2026-11-03). */
  property(at: { rowId: string; property: CellProperty; quote: string; rect: DOMRect }): void;
  /** The cell whose value is being discussed now, which shows it marked. */
  active: { rowId: string; propertyId: string } | null;
  /**
   * The words a new comment is being written on, or its draft was put away
   * for, which show marked until its draft is deleted, and once it is sent
   * until the value brings its thread's mark (Notion's, seen 2026-11-12).
   */
  pending: { rowId: string; propertyId: string; start: number; end: number; kept: boolean; sent?: boolean } | null;
  /**
   * Words pressed that threads were left on: the first of them still open,
   * as Notion's opens the open one where a resolved one overlaps it (seen
   * 2026-11-11); with none open, nothing.
   */
  open(at: { rowId: string; property: CellProperty; commentIds: readonly string[]; rect: DOMRect }): void;
  /**
   * A value's open discussions — how many, and the first, which pressing the
   * value opens — while any is open, as Notion's cell keeps its mark (seen
   * 2026-11-03).
   */
  discussed(rowId: string, propertyId: string): { count: number; commentId: string } | undefined;
  /** How many open discussions a row has as a page, not on one of its values. */
  rowDiscussions(rowId: string): number;
  /** A row's comment count pressed: its discussions, under the count (Notion's, seen 2026-11-05). */
  openRow(at: { rowId: string; rect: DOMRect }): void;
  /** Whether this reader may start a discussion here. */
  canComment: boolean;
};

export const CellCommentsContext = createContext<CellComments | null>(null);

/**
 * Whether the reader may comment on the database's page, read only or not:
 * a reader who may only read gets no "댓글" on its cells, as Notion's cells
 * don't answer a read-only guest's press (seen 2026-11-07). Without a page
 * that says, a read-only database takes no comments.
 */
export const DatabaseCommentAccessContext = createContext(false);
