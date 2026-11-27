package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The read log's records are gathered in memory and written apart from the
// reads (the 2026-10-21 decision, 22:35): a read only notes what was given
// and goes on, and a second later, or once readLogFlushRows records gather,
// whichever comes first, they are written in one transaction. A person's
// reads of one thing the same way within the ten minutes of a record add to
// its count in memory, so lists read again and again add no rows, only a
// count. Where each read falls, and its title, are found when the records
// are written, once for the many reads of a second. Everything gathered is
// written before the server stops or restarts, before a backup and before
// the log or the audit log is read; a server that dies loses at most the
// last second's reads (product/docs/self-hosting.md).
//
// readLogFlushRows: five of the 100-row statements a list's reads were
// written in, one transaction of a few milliseconds on the write connection
// where each read's list used to take its own.
const (
	readLogFlushEvery  = time.Second
	readLogFlushRows   = 500
	readLogStatement   = 100
	readLogPendingMost = 100_000
	readLogBufferKey   = "leafenReadLogBuffer"
)

// readLogPendingKey is what makes one record: who read what, how, in which
// ten minutes.
type readLogPendingKey struct {
	workspaceID string
	actor       string
	via         string
	kind        string
	id          string
	how         string
	bucket      int64
}

type readLogPending struct {
	client string
	first  int64
	last   int64
	count  int
}

type readLogBuffer struct {
	app     core.App
	mu      sync.Mutex
	pending map[readLogPendingKey]*readLogPending
	dropped int
	warned  time.Time
	// One write at a time, the timer's or one asked for.
	writing sync.Mutex
	start   sync.Once
	closing sync.Once
	wake    chan struct{}
	stop    chan struct{}
	stopped chan struct{}
}

func readLogBufferOf(app core.App) *readLogBuffer {
	// It writes as the server, though a call's read — a request's, a
	// transaction's or a snapshot's — first made it (server_own_app.go).
	server := serverAppOf(app)
	return app.Store().GetOrSet(readLogBufferKey, func() any {
		return &readLogBuffer{app: server, pending: map[readLogPendingKey]*readLogPending{},
			wake: make(chan struct{}, 1), stop: make(chan struct{}), stopped: make(chan struct{})}
	}).(*readLogBuffer)
}

// recordContentReads notes what someone was given, unless the server keeps
// no read log; the records are written apart. It never stops the read.
func recordContentReads(app core.App, workspaceID string, reader contentReader, reads []contentRead) {
	if workspaceID == "" || len(reads) == 0 || !readLogRecording(app) {
		return
	}
	readLogBufferOf(app).note(workspaceID, reader, reads, time.Now().UTC())
}

func (buffer *readLogBuffer) note(workspaceID string, reader contentReader, reads []contentRead, now time.Time) {
	actor := reader.UserID
	if actor == "" {
		actor = "share:" + reader.Client
	}
	at := now.UnixMilli()
	bucket := at / readLogBucket.Milliseconds()
	buffer.mu.Lock()
	for _, read := range reads {
		if read.ID == "" {
			continue
		}
		key := readLogPendingKey{workspaceID: workspaceID, actor: actor, via: reader.Via, kind: read.Kind, id: read.ID, how: read.How, bucket: bucket}
		if pending, ok := buffer.pending[key]; ok {
			pending.count++
			pending.last = at
			continue
		}
		if len(buffer.pending) >= readLogPendingMost {
			buffer.dropped++
			continue
		}
		buffer.pending[key] = &readLogPending{client: reader.Client, first: at, last: at, count: 1}
	}
	full := len(buffer.pending) >= readLogFlushRows
	dropped := buffer.dropped
	warn := dropped > 0 && now.Sub(buffer.warned) > time.Minute
	if warn {
		buffer.warned = now
	}
	buffer.mu.Unlock()
	if warn {
		buffer.app.Logger().Warn("leafen read log is behind: reads went unrecorded", "dropped", dropped)
	}
	buffer.start.Do(func() { go buffer.run() })
	if full {
		select {
		case buffer.wake <- struct{}{}:
		default:
		}
	}
}

func (buffer *readLogBuffer) run() {
	defer close(buffer.stopped)
	ticker := time.NewTicker(readLogFlushEvery)
	defer ticker.Stop()
	for {
		select {
		case <-buffer.stop:
			return
		case <-ticker.C:
		case <-buffer.wake:
		}
		if err := buffer.flush(); err != nil {
			buffer.app.Logger().Warn("leafen read log failed", "error", err)
		}
	}
}

// flushReadLog writes what the read log has gathered, as the log is read,
// before a backup and as the server stops.
func flushReadLog(app core.App) {
	buffer, ok := app.Store().Get(readLogBufferKey).(*readLogBuffer)
	if !ok {
		return
	}
	if err := buffer.flush(); err != nil {
		app.Logger().Warn("leafen read log failed", "error", err)
	}
}

// FlushReadLog writes the read log's gathered records, for the organization
// module's audit export.
func FlushReadLog(app core.App) { flushReadLog(app) }

// closeReadLog stops the timer and writes the rest, as the server stops.
func closeReadLog(app core.App) error {
	buffer, ok := app.Store().Get(readLogBufferKey).(*readLogBuffer)
	if !ok {
		return nil
	}
	// A timer never started stays so; one running stops before the last write.
	running := true
	buffer.start.Do(func() { running = false })
	if running {
		buffer.closing.Do(func() { close(buffer.stop) })
		<-buffer.stopped
	}
	return buffer.flush()
}

type readLogRow struct {
	id, organizationID, workspaceID, actor, kind, target string
	created, last                                        int64
	count                                                int
	via, how, client, title                              string
	places                                               []string
}

// flush writes what has gathered in one transaction; what can't be written
// stays gathered for the next.
func (buffer *readLogBuffer) flush() error {
	buffer.writing.Lock()
	defer buffer.writing.Unlock()
	buffer.mu.Lock()
	taken := buffer.pending
	buffer.pending = map[readLogPendingKey]*readLogPending{}
	buffer.mu.Unlock()
	if len(taken) == 0 {
		return nil
	}
	rows, err := buffer.resolve(taken)
	if err == nil && len(rows) > 0 {
		err = buffer.app.RunInTransaction(func(txApp core.App) error {
			for start := 0; start < len(rows); start += readLogStatement {
				if err := writeReadLogRows(txApp, rows[start:min(start+readLogStatement, len(rows))]); err != nil {
					return err
				}
			}
			return nil
		})
	}
	if err != nil {
		buffer.putBack(taken)
	}
	return err
}

func (buffer *readLogBuffer) putBack(taken map[readLogPendingKey]*readLogPending) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	for key, pending := range taken {
		if newer, ok := buffer.pending[key]; ok {
			newer.count += pending.count
			newer.first, newer.last = min(newer.first, pending.first), max(newer.last, pending.last)
			continue
		}
		buffer.pending[key] = pending
	}
}

// resolve makes the gathered reads' records, workspace by workspace, with
// the places each falls in; a workspace in no organization keeps none, and
// reads that come to one record (a page that is a database item, read as
// both) count together.
func (buffer *readLogBuffer) resolve(taken map[readLogPendingKey]*readLogPending) ([]readLogRow, error) {
	app := buffer.app
	byWorkspace := map[string][]readLogPendingKey{}
	for key := range taken {
		byWorkspace[key.workspaceID] = append(byWorkspace[key.workspaceID], key)
	}
	rows := []readLogRow{}
	byID := map[string]int{}
	for workspaceID, keys := range byWorkspace {
		var organizationID string
		if err := app.DB().NewQuery(`SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId}`).
			Bind(dbx.Params{"workspaceId": workspaceID}).Row(&organizationID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if organizationID == "" {
			continue
		}
		places := newReadPlaces(app)
		rowIDs := []string{}
		for _, key := range keys {
			if key.kind == "row" || key.kind == "page" {
				rowIDs = append(rowIDs, key.id)
			}
		}
		if err := places.loadRows(rowIDs); err != nil {
			return nil, err
		}
		for _, key := range keys {
			pending := taken[key]
			read := contentRead{Kind: key.kind, ID: key.id, How: key.how}
			if read.Kind == "page" && places.rows[read.ID] != nil {
				read.Kind = "row"
			}
			found, err := places.of(read)
			if err != nil {
				return nil, err
			}
			digest := sha256.Sum256([]byte(strings.Join([]string{"leafen-read", organizationID, key.actor, read.Kind, read.ID, key.via, read.How,
				strconv.FormatInt(key.bucket, 10)}, "\x00")))
			id := hex.EncodeToString(digest[:16])
			if at, ok := byID[id]; ok {
				rows[at].count += pending.count
				rows[at].created, rows[at].last = min(rows[at].created, pending.first), max(rows[at].last, pending.last)
				continue
			}
			byID[id] = len(rows)
			rows = append(rows, readLogRow{id: id, organizationID: organizationID, workspaceID: workspaceID, actor: key.actor,
				kind: read.Kind, target: read.ID, created: pending.first, last: pending.last, count: pending.count,
				places: found, via: key.via, how: read.How, client: pending.client, title: places.title(read)})
		}
	}
	return rows, nil
}

// writeReadLogRows writes records, adding to one already there its count
// and its last time, and the places they fall in.
func writeReadLogRows(app core.App, rows []readLogRow) error {
	values := make([]string, 0, len(rows))
	params := dbx.Params{"action": readLogAction}
	places := make(map[string][]string, len(rows))
	for index, row := range rows {
		places[row.id] = row.places
		metadata, _ := json.Marshal(map[string]any{
			"via": row.via, "how": row.how, "client": row.client, "title": row.title,
			"count": row.count, "lastAt": row.last,
		})
		at := strconv.Itoa(index)
		params["id"+at], params["organization"+at], params["actor"+at], params["kind"+at], params["target"+at] =
			row.id, row.organizationID, row.actor, row.kind, row.target
		params["created"+at], params["workspace"+at], params["metadata"+at] = row.created, row.workspaceID, string(metadata)
		values = append(values, "({:id"+at+"}, {:organization"+at+"}, {:actor"+at+"}, {:action}, {:kind"+at+"}, {:target"+at+"}, {:created"+at+
			"}, {:workspace"+at+"}, {:metadata"+at+"})")
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_organization_audit_events}}
			([[id]], [[organization_id]], [[actor_id]], [[action]], [[subject_type]], [[subject_id]], [[created]], [[workspace_id]], [[metadata_json]])
		VALUES ` + strings.Join(values, ",") + `
		ON CONFLICT ([[id]]) DO UPDATE SET [[metadata_json]] = json_set([[metadata_json]],
			'$.count', COALESCE(json_extract([[metadata_json]], '$.count'), 1) + COALESCE(json_extract(excluded.[[metadata_json]], '$.count'), 1),
			'$.lastAt', max(COALESCE(json_extract([[metadata_json]], '$.lastAt'), 0), json_extract(excluded.[[metadata_json]], '$.lastAt')))
	`).Bind(params).Execute()
	if err != nil {
		return err
	}
	return writeReadLogPlaces(app, places)
}
