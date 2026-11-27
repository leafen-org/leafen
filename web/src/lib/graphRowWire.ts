import type { GraphMutationInput, GraphOperation } from "@/contentGraph";

// A row goes to the server as its place and its values. Its icon, cover,
// full width and small text are its page's, changed on the page (PUT
// /database-rows/{rowId}/page/presentation); the server refuses a row that
// carries other ones than the row's own (content_graph_row_presentation.go),
// and a row as shown here may hold a change of them still on its way.
const rowPresentationFields = ["icon", "iconType", "cover", "coverPosition", "fullWidth", "smallText"] as const;

function rowOnTheWire(operation: GraphOperation): GraphOperation {
  if (operation.kind !== "upsertRow" || !rowPresentationFields.some((field) => field in operation.row)) return operation;
  const row = { ...operation.row };
  for (const field of rowPresentationFields) delete row[field];
  return { ...operation, row };
}

/** A content change as it goes to the server: its rows without their icons, covers and styles. */
export function graphMutationOnTheWire(input: GraphMutationInput): GraphMutationInput {
  if (!input.operations.some((operation) => operation.kind === "upsertRow")) return input;
  return { ...input, operations: input.operations.map(rowOnTheWire) };
}
