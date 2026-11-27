package app

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// How a replica's session ends (2026-11-26): whenever it closes — the server
// stopping (a quit signal, the app quitting, a restart after an update or a
// restore), replication turned off or sent elsewhere — the database's last
// changes go out first, as Litestream's closing sync, then the files' last
// list, and then the place goes. A file put in during the last second before
// a stop restores with its bytes, not as a row alone (2026-11-26: images put
// in just before a stop came back as rows without content, the files'
// sending going once a second and never at the end).
//
// The place answers past the runner's end: S3 and WebDAV send nothing once
// the context they were opened with has ended, and the runner's ends as the
// server's stop begins, so on those places neither the database's last
// changes nor the files went out at a stop. A session's place now ends
// replicaStopLimit after the stop begins or the close does, whichever is
// first, so a tick still sending as a stop begins is held to the same time
// as the close after it.

const (
	// replicaStopLimit is how long a session may take to end, within the
	// five seconds the server's stop gives the replica (app.go): the
	// database's last changes first (Litestream retries them for up to three
	// seconds), then the files.
	replicaStopLimit = 4500 * time.Millisecond
	// replicaStopListTime is kept of it for the files' list and this
	// server's note of what was sent: a file still on its way then stops,
	// and those that went are listed. Whatever is still going when the time
	// is up is cut off.
	replicaStopListTime = time.Second
)

// replicaSessionPlace is a session's place and the context its requests go
// with: it follows the start's context while the session starts, so a stop
// then cuts the start short, and outlives it once the session goes.
type replicaSessionPlace struct {
	store     scheduledBackupStore
	end       context.CancelFunc
	following func() bool
	ending    *replicaSessionEnding
}

// replicaSessionEnding is when a going session's place stops answering.
type replicaSessionEnding struct {
	once     sync.Once
	by       time.Time
	end      context.CancelFunc
	watching func() bool
}

func openReplicaSessionPlace(ctx context.Context, app core.App, stored storedScheduledBackup) (*replicaSessionPlace, error) {
	placeCtx, end := context.WithCancel(context.WithoutCancel(ctx))
	store, err := openScheduledBackupStore(placeCtx, app, stored)
	if err != nil {
		end()
		return nil, err
	}
	return &replicaSessionPlace{store: store, end: end, following: context.AfterFunc(ctx, end)}, nil
}

// outlive stops following the start's context, answering false when the
// start was stopped first; from then on the place ends replicaStopLimit
// after the context does.
func (place *replicaSessionPlace) outlive(ctx context.Context) bool {
	if !place.following() {
		return false
	}
	ending := &replicaSessionEnding{end: place.end}
	ending.watching = context.AfterFunc(ctx, func() { ending.begin() })
	place.ending = ending
	return true
}

// close lets a place go whose session didn't start.
func (place *replicaSessionPlace) close() {
	place.following()
	if place.ending != nil {
		place.ending.watching()
	}
	closeScheduledBackupStore(place.store)
	place.end()
}

// begin answers when the place stops answering, set the first time.
func (ending *replicaSessionEnding) begin() time.Time {
	ending.once.Do(func() {
		ending.by = time.Now().Add(replicaStopLimit)
		time.AfterFunc(replicaStopLimit, ending.end)
	})
	return ending.by
}

// close ends the session by its place's end. Files left unsent then stay on
// this server's disk and out of what its log database says was sent, so the
// next sending — this server's next start — sends them first; until then a
// restore elsewhere from the replica has their rows without their bytes,
// and the log says how many were left.
func (session *replicationSession) close() {
	session.ending.watching()
	ctx, cancel := context.WithDeadline(context.Background(), session.ending.begin())
	defer cancel()
	session.stopBeat()
	session.beating.Wait()
	logger := session.runner.app.Logger()
	if err := session.litestream.Close(ctx); err != nil {
		logger.Warn("replica: closing", "error", err)
	}
	if err := session.sendLastFiles(ctx); err != nil {
		logger.Warn("replica: closed before the last files reached it; they go at its next sending",
			"error", err, "limit", replicaStopLimit.String())
	}
	closeScheduledBackupStore(session.store)
	session.ending.end()
}

// sendLastFiles sends the files changed since the last tick, while the
// replica is still this server's and wasn't handed over to its standby,
// which was sent everything before it took over.
func (session *replicationSession) sendLastFiles(ctx context.Context) error {
	if session.handedOver {
		return nil
	}
	var moved errReplicaMoved
	if err := session.guard(ctx); errors.As(err, &moved) {
		return nil
	} else if err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	sending, cancel := context.WithDeadline(ctx, deadline.Add(-replicaStopListTime))
	defer cancel()
	_, err := session.files.tick(sending)
	return err
}

// readerUntil reads until ctx ends: a file being sent when a stop's time is
// up stops there.
func readerUntil(ctx context.Context) func(io.Reader) io.Reader {
	return func(source io.Reader) io.Reader { return contextReader{ctx: ctx, source: source} }
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.source.Read(buffer)
}
