package app

import (
	"context"
	"errors"
	"fmt"
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
// the floor; editing pages goes on. The floor is the databases' own size, so
// a backup snapshot of them still fits, plus room for them to grow in that
// fits the disk: a twentieth of it, at least 64 MiB and at most 2 GiB
// (2026-11-27: it was 5% of the disk, at least 2 GiB, on top of the
// databases — more than the whole 0.5 GB volume of a hosting platform's
// trial, so an empty server there refused every file, while an 8 TB disk
// kept 400 GB back).
const (
	dataDiskHeadroomMin   = uint64(64 << 20)
	dataDiskHeadroomMax   = uint64(2 << 30)
	dataDiskHeadroomShare = 20
	dataDiskCheckInterval = 5 * time.Second
	dataDiskRetryAfter    = 300
	// dataDiskLowName is the data folder's note of the disk under its floor
	// since the administrators were told (dataDiskWatch.keep). It belongs to
	// this machine (scheduledBackupMachineOnly): after a restore or a move, a
	// disk still under its floor is told again, as the inbox that held the
	// notice may not have come along.
	dataDiskLowName = ".leafen-disk-low"
)

var (
	// The disk has too little free space left for the write: freeing some
	// lets it in.
	errServerDiskLow = errors.New(errorCodeServerDiskLow)
	// The disk is too small for the write: it wouldn't fit even with nothing
	// on the disk but the floor, so only a larger disk lets it in.
	errServerDiskSmall = fmt.Errorf("%w: the disk is too small", errServerDiskLow)
)

type dataDiskWatch struct {
	mu      sync.Mutex
	checked time.Time
	free    uint64
	total   uint64
	floor   uint64
	known   bool
	lowAt   int64
	// noted is the data folder whose note of the disk under its floor was
	// read into lowAt (dataDiskWatch.noteOf).
	noted string
}

// dataDiskLow is the note kept in the data folder while the disk stays under
// its floor: when it reached it, which names the administrators' notice.
type dataDiskLow struct {
	Since int64 `json:"since"`
}

var dataDisk = &dataDiskWatch{}

// requireDataDiskRoom refuses a write that grows the data folder by incoming
// bytes when the disk would fall under its floor.
func requireDataDiskRoom(app core.App, incoming int64) error {
	disk, known := dataDisk.read(app)
	if !known {
		return nil
	}
	if incoming < 0 {
		incoming = 0
	}
	if disk.free >= disk.floor && disk.free-disk.floor >= uint64(incoming) {
		dataDisk.recovered(app, disk.free, disk.floor)
		return nil
	}
	// One file too large for the room left is refused without telling the
	// administrators; the disk itself under its floor is told once.
	if disk.free < disk.floor {
		dataDisk.reachedFloor(app, disk.free)
	}
	return disk.refusal(uint64(incoming))
}

// dataDiskRoom is how many bytes may still be written above the floor; known
// is false where free space can't be read.
func dataDiskRoom(app core.App) (room int64, known bool) {
	disk, known := dataDisk.read(app)
	if !known {
		return 0, false
	}
	if disk.free <= disk.floor {
		dataDisk.reachedFloor(app, disk.free)
		return 0, true
	}
	return int64(min(disk.free-disk.floor, uint64(1)<<62)), true
}

// dataDiskRefusal is the refusal of a write of at least incoming bytes that
// found no room above the floor: too little free space, or a disk too small.
func dataDiskRefusal(app core.App, incoming int64) error {
	disk, _ := dataDisk.read(app)
	return disk.refusal(uint64(max(incoming, 0)))
}

// dataDiskReading is the disk as last read: its free space, its size and
// the floor.
type dataDiskReading struct {
	free, total, floor uint64
}

// refusal says why a write of incoming bytes has no room: the disk is too
// small when the write wouldn't fit even with the disk holding nothing but
// the floor — the floor itself as large as the disk, or one write larger
// than all the disk holds besides it — and too full otherwise. A disk of
// unknown size is said full.
func (disk dataDiskReading) refusal(incoming uint64) error {
	if disk.total > 0 && (disk.floor >= disk.total || incoming > disk.total-disk.floor) {
		return errServerDiskSmall
	}
	return errServerDiskLow
}

// dataDiskPartsBytes is the room a file of size bytes sent in parts takes
// on the disk until it is whole: its parts as they come, and the file joined
// from them, written beside the parts before they go
// (streamAndPublishProductFileUpload). An upload asks for both when it is
// prepared, so a file the disk can't take is refused before its parts are
// sent, as too small when twice the file is more than the disk holds besides
// its floor (2026-11-27: a 290 MB file on a 0.5 GB volume was let in, its
// parts took 290 MB, and joining them failed with no word of the disk).
func dataDiskPartsBytes(size int64) int64 {
	return 2 * max(size, 0)
}

// dataDiskFloor is the free space the disk keeps: the databases' size and
// the headroom that fits a disk of total bytes.
func dataDiskFloor(total, databases uint64) uint64 {
	return databases + min(max(total/dataDiskHeadroomShare, dataDiskHeadroomMin), dataDiskHeadroomMax)
}

func (watch *dataDiskWatch) read(app core.App) (dataDiskReading, bool) {
	watch.mu.Lock()
	defer watch.mu.Unlock()
	if time.Since(watch.checked) < dataDiskCheckInterval {
		return dataDiskReading{free: watch.free, total: watch.total, floor: watch.floor}, watch.known
	}
	watch.checked = time.Now()
	free, total, ok := dataDiskSpace(app.DataDir())
	watch.known = ok
	if !ok {
		return dataDiskReading{}, false
	}
	databases := uint64(0)
	for _, name := range []string{"data.db", "auxiliary.db"} {
		if info, err := os.Stat(filepath.Join(app.DataDir(), name)); err == nil {
			databases += uint64(info.Size())
		}
	}
	watch.free, watch.total, watch.floor = free, total, dataDiskFloor(total, databases)
	return dataDiskReading{free: watch.free, total: watch.total, floor: watch.floor}, true
}

// noteOf reads, once for the data folder, the disk under its floor that the
// administrators were told of before the server last started, so a restart
// within it tells them nothing new (2026-11-27: each restart on a 0.5 GB
// disk kept under its floor put another notice in their inbox, as the
// telling lived only in memory). Called with mu held.
func (watch *dataDiskWatch) noteOf(dir string) {
	if watch.noted == dir {
		return
	}
	watch.noted = dir
	low := dataDiskLow{}
	if watch.lowAt == 0 && readRestoreFile(filepath.Join(dir, dataDiskLowName), &low) == nil {
		watch.lowAt = low.Since
	}
}

func (watch *dataDiskWatch) recovered(app core.App, free, floor uint64) {
	watch.mu.Lock()
	defer watch.mu.Unlock()
	watch.noteOf(app.DataDir())
	// Room comes back once the free space is a little above the floor, so
	// the notice isn't sent again for every file around the line.
	if watch.lowAt != 0 && free >= floor+floor/10 {
		watch.lowAt = 0
		if err := os.Remove(filepath.Join(app.DataDir(), dataDiskLowName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			app.Logger().Warn("leafen disk note not cleared", "error", err)
		}
	}
}

// reachedFloor tells the administrators once each time the disk reaches the
// floor: once while it stays under it, across restarts too, and again only
// after room came back.
func (watch *dataDiskWatch) reachedFloor(app core.App, free uint64) {
	watch.mu.Lock()
	watch.noteOf(app.DataDir())
	if watch.lowAt != 0 {
		watch.mu.Unlock()
		return
	}
	lowAt := time.Now().UnixMilli()
	watch.lowAt = lowAt
	watch.mu.Unlock()
	app.Logger().Warn("the data folder's disk is almost full; uploads, imports and exports wait for room",
		"freeBytes", free, "dataDir", app.DataDir())
	// Stopped with the server (server_background.go): a server stopping
	// tells no one and keeps no note, so its next start tells them.
	serverBackgroundOf(app).behind("disk notice", func() {
		told, err := notifyServerDiskLow(app, strconv.FormatInt(lowAt, 10), free)
		if err != nil {
			app.Logger().Warn("leafen disk notice failed", "error", err)
		}
		if told {
			watch.keep(app, lowAt)
		}
	})
}

// keep notes in the data folder that the administrators were told of the
// disk under its floor since lowAt, unless room came back meanwhile. With no
// note — no one told, or the note not written — the next start tells them.
func (watch *dataDiskWatch) keep(app core.App, lowAt int64) {
	watch.mu.Lock()
	defer watch.mu.Unlock()
	if watch.lowAt != lowAt {
		return
	}
	if err := writeRestoreFile(app.DataDir(), dataDiskLowName, dataDiskLow{Since: lowAt}); err != nil {
		app.Logger().Warn("leafen disk note not kept", "error", err)
	}
}

// notifyServerDiskLow puts the disk under its floor since episode in each
// administrator's inbox and reports whether any of them got it.
func notifyServerDiskLow(app core.App, episode string, free uint64) (bool, error) {
	administrators, err := serverAdministrators(app)
	if err != nil {
		return false, err
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
	if len(projections) == 0 {
		return false, nil
	}
	if _, err := upsertProductNotifications(context.Background(), app, projections, now); err != nil {
		return false, err
	}
	return true, nil
}

func dataDiskAmount(bytes uint64) string {
	if bytes >= 1<<30 {
		return fmt.Sprintf("%.1f GiB", float64(bytes)/(1<<30))
	}
	return fmt.Sprintf("%d MiB", bytes>>20)
}
