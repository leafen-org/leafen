package app

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's stored document has a mark that changes with every write to it:
// the database's own triggers give its head and its updates a new random
// number whenever any statement inserts, changes or deletes one of their
// rows. A document kept in memory for the page's next change
// (ygo_structural_document_cache.go) is taken again while the stored one has
// the mark it was kept under. Told by the digest of the stored parts, each
// change read the page's whole document and hashed it to find the one it
// kept was still good (2026-10-18 soak: about 9 MB read for each block
// written to a long page). A write rolled back takes its number with it,
// and a random number doesn't come again, so no kept document stands for
// one it didn't see.

// ygoMarkedStreams are the streams whose documents have marks, each a head
// table and its updates table.
var ygoMarkedStreams = []ygoStreamTables{ygoCollaborationStream, ygoStructuralStream}

func ygoMarkTriggers() map[string]string {
	triggers := map[string]string{}
	for _, stream := range ygoMarkedStreams {
		renew := func(row string) string {
			return `INSERT INTO {{leafen_ygo_marks}} ([[stream]], [[room]], [[mark]]) VALUES ('` + stream.head + `', ` + row + `.[[room]], random())
				ON CONFLICT ([[stream]], [[room]]) DO UPDATE SET [[mark]] = excluded.[[mark]];`
		}
		bump := func(row string) string {
			return `UPDATE {{leafen_ygo_marks}} SET [[mark]] = random()
				WHERE [[stream]] = '` + stream.head + `' AND [[room]] = ` + row + `.[[room]];`
		}
		triggers["leafen_ygo_marks_"+stream.head+"_insert"] = "AFTER INSERT ON {{" + stream.head + "}} BEGIN " + renew("NEW") + " END"
		triggers["leafen_ygo_marks_"+stream.head+"_update"] = "AFTER UPDATE ON {{" + stream.head + "}} BEGIN " + renew("NEW") + " END"
		triggers["leafen_ygo_marks_"+stream.head+"_delete"] = "AFTER DELETE ON {{" + stream.head + "}} BEGIN " +
			`DELETE FROM {{leafen_ygo_marks}} WHERE [[stream]] = '` + stream.head + `' AND [[room]] = OLD.[[room]]; END`
		triggers["leafen_ygo_marks_"+stream.updates+"_insert"] = "AFTER INSERT ON {{" + stream.updates + "}} BEGIN " + bump("NEW") + " END"
		triggers["leafen_ygo_marks_"+stream.updates+"_update"] = "AFTER UPDATE ON {{" + stream.updates + "}} BEGIN " + bump("NEW") + " END"
		triggers["leafen_ygo_marks_"+stream.updates+"_delete"] = "AFTER DELETE ON {{" + stream.updates + "}} BEGIN " + bump("OLD") + " END"
	}
	return triggers
}

// ygoMarkSchemaPresent says whether the marks' table is there and all their triggers.
func ygoMarkSchemaPresent(app core.App) (bool, bool, error) {
	names := []string{}
	for name := range ygoMarkTriggers() {
		names = append(names, "'"+name+"'")
	}
	var found, tables int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM [[sqlite_master]] WHERE [[type]] = 'trigger' AND [[name]] IN (` + strings.Join(names, ", ") + `)
	`).Row(&found); err != nil {
		return false, false, err
	}
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM [[sqlite_master]] WHERE [[type]] = 'table' AND [[name]] = 'leafen_ygo_marks'
	`).Row(&tables); err != nil {
		return false, false, err
	}
	return tables == 1, found == len(names), nil
}

// ygoMarksActive is whether this server's documents have marks to go by:
// the table and all its triggers there. A later schema change that rebuilt a
// document table without its triggers would leave marks that no longer
// change; without them every change reads its document afresh, and the
// server says so once.
var (
	ygoMarksFound   atomic.Bool
	ygoMarksMissing sync.Once
)

func ygoMarksActive(app core.App) bool {
	if ygoMarksFound.Load() {
		return true
	}
	table, triggers, err := ygoMarkSchemaPresent(app)
	if err == nil && table && triggers {
		ygoMarksFound.Store(true)
		return true
	}
	// Before the migration that makes them there are none to miss.
	if err != nil || table {
		ygoMarksMissing.Do(func() {
			app.Logger().Warn("ygo document marks unavailable: kept documents are not used", "error", err)
		})
	}
	return false
}

// loadYgoStreamMark is the mark the room's stored document of the stream has
// now; marked is false for a room without one, or a server without marks.
func loadYgoStreamMark(app core.App, stream ygoStreamTables, room string) (int64, bool, error) {
	if !ygoMarksActive(app) {
		return 0, false, nil
	}
	var mark int64
	err := app.DB().NewQuery(`
		SELECT [[mark]] FROM {{leafen_ygo_marks}} WHERE [[stream]] = {:stream} AND [[room]] = {:room}
	`).Bind(dbx.Params{"stream": stream.head, "room": room}).Row(&mark)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return mark, true, nil
}
