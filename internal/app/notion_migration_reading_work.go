package app

import (
	"math"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// How much a migration's reading has done, of all it will do. Reading takes
// every page and row body first, then the blocks those hold that hold more,
// then what those hold, a level at a time; each is a read of its own. Counted
// by bodies alone, a move read all its 101 bodies in two minutes and said
// about two minutes left, then "almost done" for four more minutes while it
// read what the bodies held, four reads in five (2026-11-20). So reading's
// work is every read: those done and waiting, and those still to be found —
// for each body not yet read or listed, as many blocks holding more as the
// bodies of its kind read held, and for each of those not yet read, as many
// again as those read held, weighed with the first estimate's guess of as
// many again (notion_migration_estimate.go) until enough are read. A row
// still to read goes by the rows read, weighed with the scan's sample of rows
// until enough are: the pages are read first, and what they hold is no
// measure of the rows (2026-11-21: a move's 29 pages, read first, holding a
// toggle each, reckoned its 647 rows to hold as much again, about 1,300
// reads where they took about 160, and with them the reading said some
// twenty times its time).

// notionMigrationBodies are a run's page and row bodies read and waiting,
// every read done and waiting — a body, or a block that holds more — and the
// blocks holding more found under the bodies (top) and under those (deep).
type notionMigrationBodies struct {
	Read          int `db:"read"`
	Waiting       int `db:"waiting"`
	ReadsDone     int `db:"reads_done"`
	ReadsWaiting  int `db:"reads_waiting"`
	NestedTop     int `db:"nested_top"`
	NestedTopRead int `db:"nested_top_read"`
	NestedDeep    int `db:"nested_deep"`
	NestedRead    int `db:"nested_read"`
	// The rows among them: read, waiting, and the blocks holding more found
	// under those read.
	RowsRead     int `db:"rows_read"`
	RowsWaiting  int `db:"rows_waiting"`
	RowNestedTop int `db:"row_nested_top"`
	// The scan's count of rows and its sample of them
	// (notionMigrationBodies.ofRows).
	rows, sampledRows, sampledRowNested int
}

// ofRows is the bodies with the scan's count of rows and its sample of them,
// as the run's estimate keeps them.
func (bodies notionMigrationBodies) ofRows(estimate notionMigrationEstimate) notionMigrationBodies {
	bodies.rows, bodies.sampledRows, bodies.sampledRowNested = estimate.Rows, estimate.SampledRows, estimate.SampledRowNested
	return bodies
}

// notionMigrationReadingBodies counts a run's reading while it reads. A
// block that held more was read once its place moved on (discovery_position);
// one that held nothing was never waiting.
func notionMigrationReadingBodies(app core.App, jobID, stage string) (notionMigrationBodies, error) {
	bodies := notionMigrationBodies{}
	if stage != notionMigrationStageReading {
		return bodies, nil
	}
	err := app.DB().NewQuery(`
		SELECT COALESCE(SUM([[kind]] IN ('page', 'row') AND [[discovery_status]] != 'pending'), 0) AS [[read]],
			COALESCE(SUM([[kind]] IN ('page', 'row') AND [[discovery_status]] = 'pending'), 0) AS [[waiting]],
			COALESCE(SUM([[discovery_status]] != 'pending' AND ([[kind]] IN ('page', 'row', 'template') OR [[discovery_position]] > 0)), 0) AS [[reads_done]],
			COALESCE(SUM([[discovery_status]] = 'pending'), 0) AS [[reads_waiting]],
			COALESCE(SUM([[nested]] AND [[parent_kind]] IN ('page', 'row')), 0) AS [[nested_top]],
			COALESCE(SUM([[nested]] AND [[parent_kind]] IN ('page', 'row') AND [[discovery_status]] != 'pending'), 0) AS [[nested_top_read]],
			COALESCE(SUM([[nested]] AND [[parent_kind]] = 'block'), 0) AS [[nested_deep]],
			COALESCE(SUM([[nested]] AND [[discovery_status]] != 'pending'), 0) AS [[nested_read]],
			COALESCE(SUM([[kind]] = 'row' AND [[discovery_status]] != 'pending'), 0) AS [[rows_read]],
			COALESCE(SUM([[kind]] = 'row' AND [[discovery_status]] = 'pending'), 0) AS [[rows_waiting]],
			COALESCE(SUM([[nested]] AND [[parent_kind]] = 'row'), 0) AS [[row_nested_top]]
		FROM (SELECT [[kind]], [[parent_kind]], [[discovery_status]], [[discovery_position]],
				[[kind]] NOT IN ('page', 'row', 'template') AND ([[discovery_status]] = 'pending' OR [[discovery_position]] > 0) AS [[nested]]
			FROM {{leafen_notion_import_sources}} WHERE [[job_id]] = {:jobId} AND [[kind]] IN (` + notionImportPendingSourceKindsSQL + `))
	`).Bind(dbx.Params{"jobId": jobID}).One(&bodies)
	return bodies, err
}

// notionMigrationDeepGuessReads is how many reads the guess of as many again
// weighs as, against what the blocks read held.
const notionMigrationDeepGuessReads = 10.0

// readsAll is every read reading takes, done, waiting and still to be found,
// as above, of the scan's count of bodies.
func (bodies notionMigrationBodies) readsAll(counted int) int {
	unlisted := max(0, counted-bodies.Read-bodies.Waiting)
	return bodies.ReadsDone + bodies.ReadsWaiting + unlisted + bodies.innerComing(counted)
}

// innerReads is the part of reading's work that reads what the bodies hold —
// the blocks holding more, done and all (found, and still to be found) — as
// readsAll counts it, so the reading's line says the same work its time left
// and its bar go by (2026-11-20: "41 of 41 bodies read" stood for the 17
// minutes a move read what those bodies held).
func (bodies notionMigrationBodies) innerReads(counted int) (int, int) {
	return bodies.NestedRead, bodies.NestedTop + bodies.NestedDeep + bodies.innerComing(counted)
}

// innerComing is how many blocks holding more are still to be found: for
// each body not yet read or listed, as many as the bodies of its kind read
// held, and for each of those not yet read, as many again as those read held.
func (bodies notionMigrationBodies) innerComing(counted int) int {
	unlisted := max(0, counted-bodies.Read-bodies.Waiting)
	held := 0.0
	if bodies.Read > 0 {
		held = float64(bodies.NestedTop) / float64(bodies.Read)
	}
	// The rows not yet read or listed, of the scan's count of them; the pages
	// hold as the pages read held, the rows as the rows read and the sample.
	rowsUnlisted := bodies.rowsUnlisted(counted)
	pagesRead, pagesHeld := bodies.Read-bodies.RowsRead, held
	if pagesRead > 0 {
		pagesHeld = float64(bodies.NestedTop-bodies.RowNestedTop) / float64(pagesRead)
	}
	rowsHeld := held
	if bodies.sampledRows > 0 {
		rowsHeld = float64(bodies.sampledRowNested) / float64(bodies.sampledRows)
	}
	rowsHeld = (float64(bodies.RowNestedTop) + rowsHeld*notionMigrationDeepGuessReads) / (float64(bodies.RowsRead) + notionMigrationDeepGuessReads)
	// As many again until some are read, and the first few read weighed
	// against that guess: the first block read is often a toggle holding
	// lines alone, and taken alone it once said 11 minutes left with 18 to
	// go (2026-11-20).
	deep := (float64(bodies.NestedDeep) + notionMigrationDeepGuessReads) / (float64(bodies.NestedTopRead) + notionMigrationDeepGuessReads)
	comingTop := float64(bodies.Waiting-bodies.RowsWaiting+unlisted-rowsUnlisted)*pagesHeld +
		float64(bodies.RowsWaiting+rowsUnlisted)*rowsHeld
	topUnread := float64(bodies.NestedTop-bodies.NestedTopRead) + comingTop
	return int(math.Round(comingTop + topUnread*deep))
}
