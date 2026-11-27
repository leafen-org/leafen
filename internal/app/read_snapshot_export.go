package app

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

// RunReadSnapshot runs fn in a read snapshot of its own (read_snapshot.go),
// for the organization module's reads that must see one moment of the
// database without waiting for the write connection.
func RunReadSnapshot(ctx context.Context, app core.App, fn func(core.App) error) error {
	return runReadSnapshot(ctx, app, fn)
}
