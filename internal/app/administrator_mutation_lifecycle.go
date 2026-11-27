package app

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/pocketbase/pocketbase/core"
)

const errorCodeAdministratorMutationShutdown = "LEAFEN_ADMINISTRATOR_MUTATION_SHUTDOWN_FAILED"

type administratorMutationLifecycle struct {
	mu              sync.Mutex
	shutdownContext context.Context
	shutdownCancel  context.CancelFunc
	active          int
	idle            chan struct{}
	closed          bool
}

func newAdministratorMutationLifecycle() *administratorMutationLifecycle {
	shutdownContext, shutdownCancel := context.WithCancel(context.Background())
	idle := make(chan struct{})
	close(idle)
	return &administratorMutationLifecycle{
		shutdownContext: shutdownContext,
		shutdownCancel:  shutdownCancel,
		idle:            idle,
	}
}

func (owner *administratorMutationLifecycle) Begin(
	parent context.Context,
) (context.Context, func(), error) {
	owner.mu.Lock()
	if owner.closed {
		owner.mu.Unlock()
		return nil, nil, context.Canceled
	}
	if owner.active == 0 {
		owner.idle = make(chan struct{})
	}
	owner.active++
	shutdownContext := owner.shutdownContext
	owner.mu.Unlock()

	operationContext, cancel := context.WithCancel(parent)
	stopShutdownCancellation := context.AfterFunc(shutdownContext, cancel)
	var once sync.Once
	finish := func() {
		once.Do(func() {
			stopShutdownCancellation()
			cancel()
			owner.mu.Lock()
			owner.active--
			if owner.active == 0 {
				close(owner.idle)
			}
			owner.mu.Unlock()
		})
	}
	if shutdownContext.Err() != nil {
		cancel()
	}
	return operationContext, finish, nil
}

func (owner *administratorMutationLifecycle) Shutdown(ctx context.Context) error {
	owner.mu.Lock()
	if !owner.closed {
		owner.closed = true
		owner.shutdownCancel()
	}
	idle := owner.idle
	owner.mu.Unlock()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", errorCodeAdministratorMutationShutdown, ctx.Err())
	}
}

func (owner *administratorMutationLifecycle) Wrap(
	code string,
	message string,
	next func(*core.RequestEvent) error,
) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		requestContext, finish, err := owner.Begin(event.Request.Context())
		if err != nil {
			return apiErrorAndFlush(event, http.StatusServiceUnavailable, code, message)
		}
		event.Request = event.Request.WithContext(requestContext)
		defer finish()
		return next(event)
	}
}
