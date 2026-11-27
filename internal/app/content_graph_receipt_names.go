package app

import "encoding/json"

// A content change's receipt names the page's content graph revision it made
// graphRevision, as GET /pages/{id}/content-graph reads it (snapshotRevision)
// and a change's baseGraphRevision takes it. It was named cursor, as is the
// page's own cursor that page answers and the receipt's page carry
// (page.cursor, which basePageCursor and baseCursor take): two numbers under
// one name, which agents passed one for the other (2026-11-16). The receipt
// still writes the old name, the same number, for clients that read it
// (LegacyCursor).

// graphBaseNumbersNamed says where a change's two base numbers are read, as
// its refusals name them.
const graphBaseNumbersNamed = "basePageCursor (the page's cursor, as GET /pages/{id} and a receipt's page give it) " +
	"and baseGraphRevision (its content graph's revision: GET /pages/{id}/content-graph's snapshotRevision, or a change's graphRevision)"

// MarshalJSON writes the receipt with its revision under both names.
func (receipt graphMutationReceipt) MarshalJSON() ([]byte, error) {
	type wire graphMutationReceipt
	named := wire(receipt)
	named.LegacyCursor = receipt.Cursor
	return json.Marshal(named)
}

// graphRevisionAnswer is an answer naming the content graph revision a
// change made, under its name and its old one.
func graphRevisionAnswer(answer map[string]any, revision int) map[string]any {
	answer["graphRevision"], answer["cursor"] = revision, revision
	return answer
}
