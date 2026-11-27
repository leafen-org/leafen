package app

import (
	"bytes"

	"github.com/reearth/ygo/crdt"
)

// ygoUpdateChanges is what an update brings: items, and deletions. A
// document's update after a transaction carries only what the transaction
// deleted (third_party/ygo LEAFEN_UPSTREAM.md), so one bringing neither
// changed nothing: a delivery the document had already taken in.
type ygoUpdateChanges struct {
	hasStructs bool
	hasDeletes bool
}

var ygoEmptyUpdate = crdt.EncodeStateAsUpdateV1(crdt.New(), nil)

func inspectYgoUpdateChanges(update []byte) (ygoUpdateChanges, error) {
	encodedVector, err := crdt.EncodeStateVectorFromUpdate(update)
	if err != nil {
		return ygoUpdateChanges{}, err
	}
	vector, err := crdt.DecodeStateVectorV1(encodedVector)
	if err != nil {
		return ygoUpdateChanges{}, err
	}
	deletesOnly, err := crdt.DiffUpdateV1(update, vector)
	if err != nil {
		return ygoUpdateChanges{}, err
	}
	return ygoUpdateChanges{
		hasStructs: len(vector) > 0,
		hasDeletes: !bytes.Equal(deletesOnly, ygoEmptyUpdate),
	}, nil
}
