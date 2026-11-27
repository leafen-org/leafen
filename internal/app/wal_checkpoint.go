package app

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Checkpoints mostly off the write path (2026-10-22, measured on a page of
// 1,400 blocks): SQLite checkpoints its write-ahead log from the connection
// whose commit takes it past 1,000 pages, so about one block write in
// thirteen held the one write connection some 10 ms copying the log into the
// database — two thirds of all its commits' time, mostly the two syncs each
// checkpoint takes. The write connection doesn't checkpoint by itself
// (sqlite_statement_cache.go). A connection of its own copies the log beside
// the writes, passively, never waiting for a reader or the writer; once the
// log is some twelve thousand pages long the write connection, between two
// writes, copies the little that came meanwhile, which lets the next write
// start the log over — a checkpoint beside a writer that never stops can't
// (measured: the log grew without end) — its syncs then shared by some
// hundred and sixty block writes instead of thirteen (measured: the slowest
// hundredth of writes took half as long; the log some 90 MB at most while
// written to without stop, within its 200 MB limit). Should readers keep the log from starting over, the
// write connection waits a moment for them now and then. What was committed
// is as safe as before: a checkpoint copies what the log already holds.

const (
	walCheckpointLook = 250 * time.Millisecond
	// walFinishPages is the log's length from which the write connection
	// finishes a checkpoint so that the log starts over.
	walFinishPages = 12000
	// walRestartPages is the log's length from which the write connection
	// waits a moment for readers to leave it, and walRestartWait how long.
	walRestartPages = 20000
	walRestartWait  = 20 * time.Millisecond
	walRestartAgain = 5 * time.Second
)

// registerWALCheckpoints checkpoints the data database's log while the app
// holds it open, from the moment it is opened — a command's long work as
// well as a server's — under the data's work (data_work.go), which stops it
// and waits for it before the data closes.
func registerWALCheckpoints(pb core.App) {
	dataWorkOf(pb).whileOpen("log checkpoints", checkpointWAL)
}

// checkpointWAL checkpoints whenever the log changed, until ctx ends as the
// app closes its data; a data file put in the place of the one it opened is
// opened in turn. It tells the log changed by its time: opening the log's
// files would drop, on closing, the locks SQLite holds on them in this
// process.
func checkpointWAL(ctx context.Context, app core.App) {
	path := filepath.Join(app.DataDir(), dataDatabaseFile)
	var side *sql.DB
	var opened os.FileInfo
	defer func() {
		if side != nil {
			_ = side.Close()
		}
	}()
	changed, waited, warned := time.Time{}, time.Time{}, time.Time{}
	look := time.NewTicker(walCheckpointLook)
	defer look.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-look.C:
		}
		log, err := os.Stat(path + "-wal")
		if err != nil || !log.ModTime().After(changed) {
			continue
		}
		file, err := os.Stat(path)
		if err != nil {
			continue
		}
		if side == nil || !os.SameFile(opened, file) {
			if side != nil {
				_ = side.Close()
			}
			// The driver the data database opens with, waiting for nothing.
			if side, err = sql.Open("sqlite", path+"?_pragma=busy_timeout(0)"); err != nil {
				side = nil
				continue
			}
			side.SetMaxOpenConns(1)
			opened = file
		}
		changed = log.ModTime()
		// Most of the log, beside the writes. Its answer: whether another
		// checkpoint — the replica's — held it back, the log's pages, those
		// copied.
		logged := 0
		if err := side.QueryRow(`PRAGMA wal_checkpoint(PASSIVE)`).Scan(new(int), &logged, new(int)); err != nil || logged < walFinishPages {
			continue
		}
		pages, err := finishWALCheckpoint(ctx, app, time.Since(waited) >= walRestartAgain)
		if err != nil {
			if time.Since(warned) > time.Minute {
				warned = time.Now()
				app.Logger().Debug("leafen log checkpoint waits", "error", err)
			}
			continue
		}
		if pages >= walRestartPages {
			waited = time.Now()
		}
	}
}

// finishWALCheckpoint copies the rest of the log from the write connection,
// between two writes: none comes while it copies, so the next write starts
// the log over unless a reader is still in it. A log grown long, readers
// having kept it from starting over, has the connection wait a moment for
// them when restart allows. It answers the log's length before.
func finishWALCheckpoint(ctx context.Context, app core.App, restart bool) (int, error) {
	database, ok := app.NonconcurrentDB().(*dbx.DB)
	if !ok {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := database.DB().Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	held, pages := 0, 0
	if err := conn.QueryRowContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`).Scan(&held, &pages, new(int)); err != nil || pages < walRestartPages || !restart {
		return pages, err
	}
	// The connection's own wait comes back as it was.
	timeout := 0
	if err := conn.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		return pages, err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout = `+strconv.Itoa(int(walRestartWait.Milliseconds()))); err != nil {
		return pages, err
	}
	_, restartErr := conn.ExecContext(ctx, `PRAGMA wal_checkpoint(RESTART)`)
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout = `+strconv.Itoa(timeout)); err != nil {
		return pages, err
	}
	return pages, restartErr
}
