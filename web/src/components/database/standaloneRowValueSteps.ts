import { useMemo, useRef } from "react";
import type { DatabaseRowPageSnapshot, DatabaseRowPageValue } from "@/databaseRowPageWire.generated";
import { rowTitleCaretAtEnd } from "@/lib/inlineDatabaseRequests";
import { sameCell, valueStepRecorder } from "@/lib/valueSteps";
import type { Session } from "@/types";
import { dropNativeEditorCommand, nativeEditorCommandIsLatest, recordCommittedNativeEditorCommand } from "../editor/nativeEditorUndoOwner";

/** A value a row page saved: what it held before, what it was given, and how to write either back. */
type RowValueChange = {
  propertyId: string;
  type: string;
  before: DatabaseRowPageValue | undefined;
  after: DatabaseRowPageValue;
  /** Writes it back as no step of its own; whether it was saved. */
  write(value: DatabaseRowPageValue): Promise<boolean>;
};

/**
 * Each value a database's page opened away from its database saves is a
 * step of the page's ⌘Z, as its table's cells and its peek save theirs
 * (lib/valueSteps.ts), and as Notion's row page takes a value back (seen
 * 2026-11-13: without it, ⌘Z took back the property made before the value
 * instead). ⌘Z writes back what the value held, ⌘⇧Z the value again, each
 * only over the value as its step left it: one changed since stays, which
 * the page says, as a table's cell does. A date's step is the server's
 * command, which takes back the dates it moved with it too
 * (databaseDateCommandHistory).
 */
export function useRowPageValueSteps(input: {
  session: Session;
  rowId: string;
  workspaceId: string | undefined;
  /** The row as it shows now, while the page is this row's. */
  shown(): DatabaseRowPageSnapshot | null;
  /** Says a value was changed since its step and left as it is. */
  changedSince(): void;
}) {
  const latest = useRef(input);
  latest.current = input;
  const { rowId, workspaceId } = input, userId = input.session.user.id;
  return useMemo(() => {
    // The row's own history, which its body's editor keeps (Editor.tsx).
    const scope = `${workspaceId}/${rowId}/${userId}`;
    async function restore(changes: readonly RowValueChange[], forward: boolean) {
      const shown = latest.current.shown();
      if (shown?.row.id !== rowId) return false;
      let saved = true;
      for (const change of changes) {
        if (!sameCell(shown.row.values[change.propertyId], forward ? change.before : change.after)) {
          latest.current.changedSince();
          continue;
        }
        const value = (forward ? change.after : change.before) ?? { type: change.type, value: null };
        const saving = change.write(value);
        // A title taken back or made again takes the caret to its end, as Notion's (seen 2026-11-13).
        if (change.type === "title") requestAnimationFrame(() => rowTitleCaretAtEnd(rowId));
        saved = await saving && saved;
      }
      return saved;
    }
    const record = valueStepRecorder<RowValueChange>({
      cell: (change) => change.propertyId,
      record: (key, changes) => void recordCommittedNativeEditorCommand(scope, key, (forward) => restore(changes, forward)),
      isLatest: (key) => nativeEditorCommandIsLatest(scope, key),
      drop: (key) => dropNativeEditorCommand(scope, key),
    });
    /** Notes a save as a step as it shows; what it gives takes the step out again if the server refuses it. */
    return (change: RowValueChange) => (
      !workspaceId || change.type === "date" || sameCell(change.before, change.after) ? undefined : record([change])
    );
  }, [rowId, userId, workspaceId]);
}
