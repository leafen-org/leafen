package app

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// One write is one moment in a page's history, as Notion's: every version a
// call through a connection records — an MCP tool's, a key's, a public
// connection's — carries the time of its first, and every version one
// transaction records the time of its first. A database an MCP client made
// came as a page and then its database, and a row as its values and then
// its body, versions some milliseconds apart, and get-version-changes named
// their records first edited at the one and last at the other, where
// Notion's names one time for all one call made (seen 2026-11-14: a
// database's data source, block and view; two rows made together, and a
// row's values and blocks).

// pageHistoryCallMoment is the time a call's versions carry, set by its
// first.
type pageHistoryCallMoment struct{ at atomic.Int64 }

type pageHistoryCallMomentKey struct{}

// withPageHistoryCallMoment makes ctx's call one moment in the histories it
// writes; a call that is one already stays it.
func withPageHistoryCallMoment(ctx context.Context) context.Context {
	if _, held := ctx.Value(pageHistoryCallMomentKey{}).(*pageHistoryCallMoment); held {
		return ctx
	}
	return context.WithValue(ctx, pageHistoryCallMomentKey{}, &pageHistoryCallMoment{})
}

// pageHistoryCallMoments holds, by open transaction, the call moment its
// versions carry, named with its connection (namePageHistoryConnection).
var pageHistoryCallMoments sync.Map

// holdPageHistoryCallMoment makes the versions txApp records carry ctx's
// call moment until the returned release; a transaction already holding one
// keeps it.
func holdPageHistoryCallMoment(txApp core.App, ctx context.Context) func() {
	if ctx == nil {
		return func() {}
	}
	moment, ok := ctx.Value(pageHistoryCallMomentKey{}).(*pageHistoryCallMoment)
	if !ok {
		return func() {}
	}
	if _, held := pageHistoryCallMoments.LoadOrStore(txApp, moment); held {
		return func() {}
	}
	return func() { pageHistoryCallMoments.Delete(txApp) }
}

// pageHistoryTransactionMoments holds, by open transaction, the time its
// versions carry when no call names one.
var pageHistoryTransactionMoments sync.Map

// pageHistoryMoment is the time a version app records now carries, in
// milliseconds: its call's, else its transaction's, the first time one
// asks, or now outside both.
func pageHistoryMoment(app core.App) int64 {
	now := time.Now().UTC().UnixMilli()
	// The typing a block change carries from before it is a write of its
	// own, before the change's (page_history_block_change_text.go).
	if pageHistoryTypingBeforeBlockChange(app) {
		return now
	}
	var call *pageHistoryCallMoment
	if keyed, ok := app.(userKeyApp); ok && keyed.ctx != nil {
		call, _ = keyed.ctx.Value(pageHistoryCallMomentKey{}).(*pageHistoryCallMoment)
	} else if held, ok := pageHistoryCallMoments.Load(app); ok {
		call, _ = held.(*pageHistoryCallMoment)
	}
	if call != nil {
		if call.at.CompareAndSwap(0, now) {
			return now
		}
		return call.at.Load()
	}
	return holdPageHistoryTransactionMoment(app, now)
}

// holdPageHistoryTransactionMoment makes the versions app's transaction
// records carry at, unless they carry a moment already, and gives the moment
// they carry; at outside a transaction.
func holdPageHistoryTransactionMoment(app core.App, at int64) int64 {
	transaction := app.TxInfo()
	if transaction == nil {
		return at
	}
	held, taken := pageHistoryTransactionMoments.LoadOrStore(transaction, at)
	if !taken {
		transaction.OnComplete(func(error) error {
			pageHistoryTransactionMoments.Delete(transaction)
			return nil
		})
	}
	return held.(int64)
}
