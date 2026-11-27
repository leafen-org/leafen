package app

import (
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The text a change of a page's blocks writes in its own transaction is that
// change's, not the typing of the minute before it: the text of the blocks it
// removes, which the content graph empties (applyContentGraphTextUpdates);
// the words the page's editor sends with it — a removed block's text emptied
// in the collaborative text, a line split by Enter or merged into the one
// above (content_graph_mutation_words.go); and the text of the blocks it
// makes. It is a version of its own at the change's moment, beside the
// change's graph version (page_history_moments.go), and a revert of the
// change takes the two as one write (pageHistoryWriteStart). Taken into the
// typing version of the minute (extendPageHistoryCollaborationVersion), a
// block removed from the editor's block menu, or by a session's deleteBlock,
// within the minute it was typed in emptied that typing's own version: the
// words typed were in no version, restoring a version never showed them, and
// reverting the removal brought the block back empty (2026-11-18).
//
// The typing the editor wrote before the change, which waited for the block
// changes before it and goes with this one (content_graph_mutation_words.go),
// is not the change's: stored first in its transaction, it is typing, which
// goes on in the minute's typing version where typing may, and else a
// version of its own at its own moment, the change's write beginning after
// it. Taken as the change's own text, a line typed in the second after Enter
// and removed then was in no version, and reverting the removal, which finds
// its block as the write before it left it, brought it back empty
// (2026-11-18).

// pageHistoryTypingAhead holds the transactions storing now the typing a
// change of a page's blocks carries from before it.
var pageHistoryTypingAhead sync.Map

// asTypingBeforeBlockChange runs store, which stores in app's transaction the
// typing a change of a page's blocks carries from before it, as that typing,
// before the change records any version; the change's versions then carry a
// later moment than the typing's.
func asTypingBeforeBlockChange(app core.App, store func() error) error {
	transaction := app.TxInfo()
	if transaction == nil {
		return store()
	}
	err := func() error {
		pageHistoryTypingAhead.Store(transaction, struct{}{})
		defer pageHistoryTypingAhead.Delete(transaction)
		return store()
	}()
	if err == nil {
		holdPageHistoryTransactionMoment(app, time.Now().UTC().UnixMilli()+1)
	}
	return err
}

// pageHistoryTypingBeforeBlockChange is whether app stores now the typing a
// change of a page's blocks carries from before it.
func pageHistoryTypingBeforeBlockChange(app core.App) bool {
	transaction := app.TxInfo()
	if transaction == nil {
		return false
	}
	_, ahead := pageHistoryTypingAhead.Load(transaction)
	return ahead
}

// pageHistoryBlockChangeText is whether app writes in the transaction of a
// change of a page's blocks, besides the typing before it: the content
// graph's, the one transaction that keeps its lookups for its length
// (rememberWriteLookups).
func pageHistoryBlockChangeText(app core.App) bool {
	transaction := app.TxInfo()
	if transaction == nil || pageHistoryTypingBeforeBlockChange(app) {
		return false
	}
	writeLookupMemos.Lock()
	defer writeLookupMemos.Unlock()
	_, kept := writeLookupMemos.byTransaction[transaction]
	return kept
}
