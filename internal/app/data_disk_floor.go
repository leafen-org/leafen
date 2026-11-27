package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The data folder's disk keeps room for the database, its log and a backup
// (2026-10-18: nothing looked at free space, so anyone who could upload —
// a guest on one page, or anyone where sign-up is open — could fill it, and
// then every save on the server stopped). Writes others grow — an upload,
// an import, an export — are refused while the free space would fall under
// the floor; editing pages goes on. The floor is 5% of the disk, at least 2
// GiB, plus the databases' own size, so a backup snapshot still fits.
const (
	dataDiskFloorMinBytes = uint64(2 << 30)
	dataDiskFloorPercent  = 5
	dataDiskCheckInterval = 5 * time.Second
	dataDiskRetryAfter    = 300
)

var errServerDiskLow = errors.New(errorCodeServerDiskLow)

type dataDiskWatch struct {
	mu      sync.Mutex
	checked time.Time
	free    uint64
	floor   uint64
	known   bool
	lowAt   int64
}

var dataDisk = &dataDiskWatch{}

// requireDataDiskRoom refuses a write that grows the data folder by incoming
// bytes when the disk would fall under its floor.
func requireDataDiskRoom(app core.App, incoming int64) error {
	free, floor, known := dataDisk.read(app)
	if !known {
		return nil
	}
	if incoming < 0 {
		incoming = 0
	}
	if free >= floor && free-floor >= uint64(incoming) {
		dataDisk.recovered(free, floor)
		return nil
	}
	// One file too large for the room left is refused without telling the
	// administrators; the disk itself under its floor is told once.
	if free < floor {
		dataDisk.reachedFloor(app, free)
	}
	return errServerDiskLow
}

// dataDiskRoom is how many bytes may still be written above the floor; known
// is false where free space can't be read.
func dataDiskRoom(app core.App) (room int64, known bool) {
	free, floor, known := dataDisk.read(app)
	if !known {
		return 0, false
	}
	if free <= floor {
		dataDisk.reachedFloor(app, free)
		return 0, true
	}
	return int64(min(free-floor, uint64(1)<<62)), true
}

func (watch *dataDiskWatch) read(app core.App) (uint64, uint64, bool) {
	watch.mu.Lock()
	defer watch.mu.Unlock()
	if time.Since(watch.checked) < dataDiskCheckInterval {
		return watch.free, watch.floor, watch.known
	}
	watch.checked = time.Now()
	free, total, ok := dataDiskSpace(app.DataDir())
	watch.known = ok
	if !ok {
		return 0, 0, false
	}
	floor := max(dataDiskFloorMinBytes, total/100*dataDiskFloorPercent)
	for _, name := range []string{"data.db", "auxiliary.db"} {
		if info, err := os.Stat(filepath.Join(app.DataDir(), name)); err == nil {
			floor += uint64(info.Size())
		}
	}
	watch.free, watch.floor = free, floor
	return free, floor, true
}

func (watch *dataDiskWatch) recovered(free, floor uint64) {
	watch.mu.Lock()
	defer watch.mu.Unlock()
	// Room comes back once the free space is a little above the floor, so
	// the notice isn't sent again for every file around the line.
	if watch.lowAt != 0 && free >= floor+floor/10 {
		watch.lowAt = 0
	}
}

// reachedFloor tells the administrators once each time the disk reaches the
// floor.
func (watch *dataDiskWatch) reachedFloor(app core.App, free uint64) {
	watch.mu.Lock()
	if watch.lowAt != 0 {
		watch.mu.Unlock()
		return
	}
	watch.lowAt = time.Now().UnixMilli()
	episode := strconv.FormatInt(watch.lowAt, 10)
	watch.mu.Unlock()
	app.Logger().Warn("the data folder's disk is almost full; uploads, imports and exports wait for room",
		"freeBytes", free, "dataDir", app.DataDir())
	// Stopped with the server (server_background.go).
	serverBackgroundOf(app).behind("disk notice", func() { notifyServerDiskLow(app, episode, free) })
}

func notifyServerDiskLow(app core.App, episode string, free uint64) {
	administrators, err := serverAdministrators(app)
	if err != nil {
		app.Logger().Warn("leafen disk notice failed", "error", err)
		return
	}
	now := time.Now().UnixMilli()
	key := "server-disk:" + appNoticeID("server-disk", episode)
	projections := []productNotificationProjection{}
	for _, administrator := range administrators {
		workspaceID, err := personNoticeWorkspace(app, administrator, "")
		if err != nil || workspaceID == "" {
			continue
		}
		projections = append(projections, productNotificationProjection{
			ID: appNoticeID("server-disk", episode, administrator), EventID: appNoticeID("server-disk-event", episode),
			WorkspaceID: workspaceID, UserID: administrator, ActivityKey: key, Kind: "system",
			PageID: agentNoticePagePrefix + "server-disk", Title: dataDiskAmount(free),
			Target: "/admin/settings", OccurredAt: now,
		})
	}
	if len(projections) > 0 {
		if _, err := upsertProductNotifications(context.Background(), app, projections, now); err != nil {
			app.Logger().Warn("leafen disk notice failed", "error", err)
		}
	}
}

func dataDiskAmount(bytes uint64) string {
	if bytes >= 1<<30 {
		return fmt.Sprintf("%.1f GiB", float64(bytes)/(1<<30))
	}
	return fmt.Sprintf("%d MiB", bytes>>20)
}

// writeServerDiskLow answers a write refused for the disk: 507, the reason,
// and when to look again.
func writeServerDiskLow(event *core.RequestEvent, operation string, cause error) error {
	event.Response.Header().Set("Retry-After", strconv.Itoa(dataDiskRetryAfter))
	return apiErrorWithCause(event, http.StatusInsufficientStorage, errorCodeServerDiskLow,
		"The server's disk is almost full. Ask the server's administrator to free space; this works again once there is room.",
		operation, "disk-floor", errorCodeServerDiskLow, cause)
}
