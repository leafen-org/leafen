import { databaseRowPageErrorCodes } from "@/databaseRowPageWire.generated";
import type { ProductErrorMeaning } from "@/lib/productErrorMeaningCatalog";
import { productErrorText } from "@/lib/productErrorText";

// Why a move was refused, said by its reason: a database's page goes to
// another page only for someone with full access to it who edits where it
// goes (2026-10-31); nothing moves into, or links to, another workspace — a
// row into its database, a page under its page, a relation, a linked view —
// but a whole page, by Move to's workspace choice (2026-11-26, the server's
// LEAFEN_OTHER_WORKSPACE, other_workspace_refusal.go).
export type MoveRefusalClass = "row-move-denied" | "other-workspace";

export const otherWorkspaceRefusalCode = "LEAFEN_OTHER_WORKSPACE";

export function moveRefusalClass(code: string): MoveRefusalClass | null {
  if (code === databaseRowPageErrorCodes.moveDenied) return "row-move-denied";
  if (code === otherWorkspaceRefusalCode) return "other-workspace";
  return null;
}

// Each reason's cause, what to do, and when trying again helps.
const MEANINGS: Record<MoveRefusalClass, readonly [string, string, string]> = {
  "row-move-denied": ["rowMoveNeedsFullAccess", "askFullAccessHolderToMoveIt", "rowMoveRefusedUntilAccess"],
  "other-workspace": ["otherWorkspaceCantMoveOrLink", "makeItAnewInThatWorkspace", "otherWorkspaceRefusedAgain"],
};

export function moveRefusalMeaning(
  classification: string,
  operation: string,
  deniedEffect: string,
): ProductErrorMeaning | null {
  const keys = MEANINGS[classification as MoveRefusalClass];
  if (!keys) return null;
  const [cause, nextAction, retry] = keys.map((key) => productErrorText(`meanings.${key}`));
  return {
    cause, effect: deniedEffect, known: true, nextAction, operation, retry,
    retryDisposition: "safe-after-action",
  };
}
