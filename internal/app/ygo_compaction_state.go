package app

import "github.com/reearth/ygo/crdt"

// compactedYgoState is a document's stored state and the updates after it as
// one state: merged, read into a document — the check that they hold one,
// as every reader reads them (loadYgoStreamDoc) — and kept as that document
// encodes itself (encodedYgoState). A merge alone keeps the content of
// everything ever deleted: a document reads it and lets it go, so each read
// paid for it again (2026-11-18: a page whose 8,400 blocks went to the trash
// kept their text, 6.5 MB of 6.9; its structure 1.5 MB of which 0.6 held
// anything, read by every open, checkpoint and version restore).
func compactedYgoState(parts [][]byte) ([]byte, error) {
	merged, err := crdt.MergeUpdatesV1(parts...)
	if err != nil {
		return nil, err
	}
	document := crdt.New()
	if err := crdt.ApplyUpdateV1(document, merged, nil); err != nil {
		return nil, err
	}
	return encodedYgoState(document, merged), nil
}

// encodedYgoState is what to keep of a document read from merged: the
// document as it encodes itself, without the content of what was deleted. A
// document holding parts it couldn't place yet keeps the merge as it was,
// whose bytes hold them as they came.
func encodedYgoState(document *crdt.Doc, merged []byte) []byte {
	if pending := document.PendingStats(); pending.Items > 0 || pending.DeleteRanges > 0 {
		return merged
	}
	return crdt.EncodeStateAsUpdateV1(document, nil)
}
