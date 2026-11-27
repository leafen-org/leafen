package app

import (
	"context"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// beginShutdown begins the closing of the collaboration's storage, ctx
// ending at the server's closing deadline, at stop's times (serverStop,
// shutdown.go): the writes under way get until stop.underWay to end and are
// then stopped (shutdownContext); those begun once it is closing — each
// room's last flush among them — until stop.closing (closingStop), so one a
// slow write holds is stopped and rolled back before the waits for the write
// connection end, and the parts' last writes queued behind it are written,
// while the rest of the server still ends before the deadline. (2026-11-18: a
// room's last flush held by a slow write ran to the deadline, and the server
// ended with code 1.)
func (p *pocketBaseYgoPersistence) beginShutdown(ctx context.Context, stop serverStop) {
	closingStop := ctx
	if !stop.closing.IsZero() {
		var release context.CancelFunc
		closingStop, release = context.WithDeadline(ctx, stop.closing)
		context.AfterFunc(ctx, release)
	}
	p.shutdownMu.Lock()
	if p.closingStop == nil {
		p.closingStop = closingStop
	}
	idle := p.preShutdownIdle
	p.shutdownMu.Unlock()
	if remaining := time.Until(stop.underWay); !stop.underWay.IsZero() && remaining > 0 {
		timer := time.NewTimer(remaining)
		select {
		case <-idle:
		case <-timer.C:
		case <-ctx.Done():
		}
		timer.Stop()
	}
	p.shutdownCancel()
}

// madeWhole is the context a change asked for is made under: its asker's
// values — who makes it, through which connection, its write lane — without
// its asker's end. A change whose request was read whole is the server's to
// make though the connection goes before its answer: a browser killed as its
// last words went in a request that outlives the page, a phone's app ended by
// its system (2026-11-27: a row's title sent so, waiting for its page's
// writer behind a larger change, was dropped 4 times in 4, on the content and
// the row's value routes). Only the closing server stops it
// (operationContext); a job stopped meanwhile stops between its changes.
func madeWhole(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithoutCancel(ctx)
}

func linkedYgoOperationContext(parent, cancellation context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(cancellation, cancel)
	if cancellation.Err() != nil {
		cancel()
	}
	return ctx, func() {
		stop()
		cancel()
	}
}

func (p *pocketBaseYgoPersistence) operationContext(
	parent context.Context,
	room string,
) (context.Context, func(), bool, error) {
	p.shutdownMu.Lock()
	closingStop := p.closingStop
	_, aborted := p.shutdownAborted[room]
	if closingStop != nil {
		p.shutdownMu.Unlock()
		if aborted {
			return nil, nil, false, context.Canceled
		}
		ctx, cancel := linkedYgoOperationContext(parent, closingStop)
		return ctx, cancel, false, nil
	}
	if p.preShutdownActive == 0 {
		p.preShutdownIdle = make(chan struct{})
	}
	p.preShutdownActive++
	p.shutdownMu.Unlock()
	ctx, cancel := linkedYgoOperationContext(parent, p.shutdownContext)
	return ctx, func() {
		cancel()
		p.finishPreShutdownOperation()
	}, true, nil
}

// operationStop is what stops an operation operationContext began, but its
// caller's own end: the closing server's signal for one begun before the
// server began closing, the closing writes' stop for one begun after
// (beginShutdown).
func (p *pocketBaseYgoPersistence) operationStop(startedBeforeShutdown bool) context.Context {
	if startedBeforeShutdown {
		return p.shutdownContext
	}
	p.shutdownMu.Lock()
	defer p.shutdownMu.Unlock()
	return p.closingStop
}

// runOperationTransaction runs an operation's write in a transaction whose
// statements stop with stop (sqlite_write_gate.go): a write still running
// when a closing server stops it is rolled back and refused as canceled
// before the server ends, rather than ended with it unanswered. A write
// whose commit began is kept.
func (p *pocketBaseYgoPersistence) runOperationTransaction(stop context.Context, write func(core.App) error) error {
	err := p.app.RunInTransaction(func(txApp core.App) error {
		defer interruptWriteStatementsOn(p.app, stop)()
		return write(txApp)
	})
	if err != nil && stop != nil && stop.Err() != nil {
		return context.Canceled
	}
	return err
}

func (p *pocketBaseYgoPersistence) finishPreShutdownOperation() {
	p.shutdownMu.Lock()
	p.preShutdownActive--
	if p.preShutdownActive == 0 {
		close(p.preShutdownIdle)
	}
	p.shutdownMu.Unlock()
}

func (p *pocketBaseYgoPersistence) markShutdownAborted(room string) {
	p.shutdownMu.Lock()
	p.shutdownAborted[room] = struct{}{}
	p.shutdownMu.Unlock()
}
