package app

import (
	"sync"

	"github.com/pocketbase/pocketbase/core"
)

// A block's write records two versions of its page — its structure's and its
// text's — and each asked again what nothing in the write can change: whether
// the workspace's webhooks want the kind of event, which app the workspace
// is (2026-10-21: a statement each, every block, while the write held the one
// write connection). A content change's transaction keeps those answers for
// its length; any other transaction, which might change them, asks each time.

var writeLookupMemos = struct {
	sync.Mutex
	byTransaction map[*core.TxAppInfo]map[any]any
}{byTransaction: map[*core.TxAppInfo]map[any]any{}}

// rememberWriteLookups keeps the lookups txApp's content change asks, until
// its transaction ends. The page's history tells a content change's text from
// typing by it (pageHistoryBlockChangeText).
func rememberWriteLookups(txApp core.App) {
	transaction := txApp.TxInfo()
	if transaction == nil {
		return
	}
	writeLookupMemos.Lock()
	_, known := writeLookupMemos.byTransaction[transaction]
	if !known {
		writeLookupMemos.byTransaction[transaction] = map[any]any{}
	}
	writeLookupMemos.Unlock()
	if !known {
		transaction.OnComplete(func(error) error {
			writeLookupMemos.Lock()
			delete(writeLookupMemos.byTransaction, transaction)
			writeLookupMemos.Unlock()
			return nil
		})
	}
}

// writeLookup answers what compute does, once a content change's
// transaction; elsewhere each time.
func writeLookup[V any](app core.App, key any, compute func() (V, error)) (V, error) {
	transaction := app.TxInfo()
	if transaction == nil {
		return compute()
	}
	writeLookupMemos.Lock()
	memo, kept := writeLookupMemos.byTransaction[transaction]
	value, found := memo[key]
	writeLookupMemos.Unlock()
	if !kept {
		return compute()
	}
	if found {
		return value.(V), nil
	}
	computed, err := compute()
	if err == nil {
		writeLookupMemos.Lock()
		memo[key] = computed
		writeLookupMemos.Unlock()
	}
	return computed, err
}
