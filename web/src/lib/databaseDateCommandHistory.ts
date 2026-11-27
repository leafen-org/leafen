import { recordCommittedNativeEditorCommand } from "@/components/editor/nativeEditorUndoOwner";
import type { ProductPages } from "@/components/product/productPagesContext";
import { writeDatabaseRowPageValues } from "@/databaseRowPageApi";
import type { DatabaseRowPageActionResponse, DatabaseRowPageSnapshot } from "@/databaseRowPageWire.generated";
import { newId } from "@/lib/ids";
import type { Session } from "@/types";

/** A row peek records on its host; a standalone row records on its own body. */
export function recordRowDateCommand(input: {
  session: Session;
  snapshot: DatabaseRowPageSnapshot;
  response: DatabaseRowPageActionResponse;
  product: ProductPages | null;
  isCurrent(): boolean;
  reload(): Promise<boolean>;
  failed(): void;
}) {
  const { response, session, snapshot, product } = input;
  if (!response.dateCommand) return;
  const key = `${response.containerPageId}/${response.dateCommand}`;
  const write = async (forward: boolean) => {
    await writeDatabaseRowPageValues(session, snapshot.row.id, {
      mutationId: newId(), values: {}, restoreDateCommand: response.dateCommand, redo: forward,
    });
  };
  // The host owns a closed peek's command too. Its callback fences the page
  // and principal; only rereading the peek depends on that peek still living.
  if (product?.recordDateCommand?.(key, async (forward) => {
    await write(forward);
    return input.isCurrent() ? input.reload() : true;
  })) return;
  if (!input.isCurrent()) return;
  recordCommittedNativeEditorCommand(`${snapshot.workspaceId}/${snapshot.row.id}/${session.user.id}`, key, async (forward) => {
    if (!input.isCurrent()) return false;
    try { await write(forward); return input.isCurrent() && await input.reload() && input.isCurrent(); }
    catch { if (input.isCurrent()) input.failed(); return false; }
  });
}
