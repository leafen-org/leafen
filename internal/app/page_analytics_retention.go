package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	pageAnalyticsRetentionBatchSize = 32

	pageAnalyticsRetentionStatusIdle   = "idle"
	pageAnalyticsRetentionStatusActive = "active"

	pageAnalyticsRetentionPhaseBatches = "batches"
	pageAnalyticsRetentionPhaseDaily   = "daily"
	pageAnalyticsRetentionPhaseUnique  = "unique"
	pageAnalyticsRetentionPhaseRecent  = "recent"
)

const pageAnalyticsTimeLayout = "2006-01-02T15:04:05.000Z"

type pageAnalyticsRetentionTurn struct {
	Deleted int
	HasMore bool
	NextDue int64
}

type storedPageAnalyticsRetentionState struct {
	Status     string `db:"status"`
	Phase      string `db:"phase"`
	BoundaryAt string `db:"boundary_at"`
	CursorTime string `db:"cursor_time"`
	CursorA    string `db:"cursor_a"`
	CursorB    string `db:"cursor_b"`
	CursorC    string `db:"cursor_c"`
	CursorD    string `db:"cursor_d"`
	CursorE    string `db:"cursor_e"`
	Deleted    int    `db:"deleted_count"`
	NextDueAt  string `db:"next_due_at"`
}

type pageAnalyticsRetentionCandidate struct {
	AuthorityTime string `db:"authority_time"`
	Days          int    `db:"days"`
	Held          bool   `db:"held"`
	CursorA       string `db:"cursor_a"`
	CursorB       string `db:"cursor_b"`
	CursorC       string `db:"cursor_c"`
	CursorD       string `db:"cursor_d"`
	CursorE       string `db:"cursor_e"`
}

type pageAnalyticsRetentionPhaseSpec struct {
	Table       string
	DeleteFrom  string
	Index       string
	DeleteIndex string
	TimeColumn  string
	Keys        []string
	// Cutoff is as far as the sweep looks, by the shortest period any
	// organization keeps; each row then goes by its own workspace's
	// organization's period. Shift is how far the time column runs ahead
	// of the view it counts: a daily row's retention_at is a year after it.
	Cutoff string
	Shift  int
	// Page and Person are the columns naming the page a row is about and
	// the person whose view or search it is, for legal holds ("" for none).
	Page   string
	Person string
}

// pageAnalyticsRetentionScanDays caps the period a sweep looks back by at a
// century: a longer one finds nothing older, and each row is still checked
// against its own period.
const pageAnalyticsRetentionScanDays = 36525

func pageAnalyticsRetentionCutoff(boundary time.Time, shift, days int) string {
	return pageAnalyticsTimestamp(boundary.AddDate(0, 0, shift-min(days, pageAnalyticsRetentionScanDays)))
}

// pageAnalyticsRetentionExpired says whether a row is past its workspace's
// organization's period at the sweep's boundary; an organization keeping its
// analytics keeps every row, and a legal hold the rows it covers.
func pageAnalyticsRetentionExpired(candidate pageAnalyticsRetentionCandidate, spec pageAnalyticsRetentionPhaseSpec, boundary time.Time) bool {
	at, valid := parsePageAnalyticsRetentionTime(candidate.AuthorityTime)
	if candidate.Days <= 0 || candidate.Held || !valid {
		return false
	}
	return at.UnixMilli()+int64(candidate.Days-spec.Shift)*activityRetentionDay <= boundary.UnixMilli()
}

func pageAnalyticsTimestamp(value time.Time) string {
	return value.UTC().Truncate(time.Millisecond).Format(pageAnalyticsTimeLayout)
}

func (runner *pageAnalyticsRetentionRunner) runTurn(
	ctx context.Context,
	now time.Time,
) (pageAnalyticsRetentionTurn, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	if err := ctx.Err(); err != nil {
		return pageAnalyticsRetentionTurn{}, err
	}
	now = now.UTC().Truncate(time.Millisecond)
	turn := pageAnalyticsRetentionTurn{}
	err := runner.app.RunInTransaction(func(txApp core.App) error {
		state, err := loadPageAnalyticsRetentionState(ctx, txApp, now)
		if err != nil {
			return err
		}
		if state.Status == pageAnalyticsRetentionStatusIdle {
			if due, valid := parsePageAnalyticsRetentionTime(state.NextDueAt); valid && due.After(now) {
				turn.NextDue = due.UnixMilli()
				return nil
			}
			state, err = startPageAnalyticsRetentionSweep(ctx, txApp, now)
			if err != nil {
				return err
			}
		}
		boundary, valid := parsePageAnalyticsRetentionTime(state.BoundaryAt)
		if !valid {
			return errors.New("page analytics retention boundary is invalid")
		}
		shortest, err := shortestWorkspaceRetentionDays(txApp, "analytics_days")
		if err != nil {
			return err
		}
		spec, ok := pageAnalyticsRetentionSpec(state.Phase, boundary, shortest)
		if !ok {
			return errors.New("page analytics retention phase is invalid")
		}
		candidates, hasMore := []pageAnalyticsRetentionCandidate{}, false
		if shortest > 0 {
			candidates, hasMore, err = selectPageAnalyticsRetentionCandidates(ctx, txApp, state, spec)
			if err != nil {
				return err
			}
		}
		expired := []pageAnalyticsRetentionCandidate{}
		for _, candidate := range candidates {
			if pageAnalyticsRetentionExpired(candidate, spec, boundary) {
				expired = append(expired, candidate)
			}
		}
		if len(expired) > 0 {
			if err := deletePageAnalyticsRetentionCandidates(ctx, txApp, spec, expired); err != nil {
				return err
			}
			turn.Deleted = len(expired)
		}
		if hasMore {
			// The walk goes on past the rows a longer period keeps.
			last := candidates[len(candidates)-1]
			if err := updatePageAnalyticsRetentionCursor(ctx, txApp, state, last, now, len(expired)); err != nil {
				return err
			}
			turn.HasMore = true
			return nil
		}
		nextPhase, final := nextPageAnalyticsRetentionPhase(state.Phase)
		if !final {
			if err := advancePageAnalyticsRetentionPhase(ctx, txApp, state, nextPhase, now, len(expired)); err != nil {
				return err
			}
			turn.HasMore = true
			return nil
		}
		nextDue := now.Add(24 * time.Hour)
		if shortest > 0 {
			nextDue, err = earliestPageAnalyticsRetentionDue(ctx, txApp, shortest)
			if err != nil {
				return err
			}
			nextDue = activityRetentionFallbackDue(nextDue, now)
		}
		if err := completePageAnalyticsRetentionSweep(ctx, txApp, state, now, len(expired), nextDue); err != nil {
			return err
		}
		if !nextDue.IsZero() {
			turn.NextDue = nextDue.UnixMilli()
		}
		return nil
	})
	return turn, err
}

func loadPageAnalyticsRetentionState(
	ctx context.Context,
	app core.App,
	now time.Time,
) (storedPageAnalyticsRetentionState, error) {
	state := storedPageAnalyticsRetentionState{}
	err := app.DB().NewQuery(`
		SELECT [[status]],[[phase]],[[boundary_at]],[[cursor_time]],[[cursor_a]],
			[[cursor_b]],[[cursor_c]],[[cursor_d]],[[cursor_e]],[[deleted_count]],[[next_due_at]]
		FROM {{leafen_page_analytics_retention_state}} WHERE [[id]]='global'
	`).WithContext(ctx).One(&state)
	if !errors.Is(err, sql.ErrNoRows) {
		return state, err
	}
	timestamp := pageAnalyticsTimestamp(now)
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_page_analytics_retention_state}} (
			[[id]],[[status]],[[phase]],[[boundary_at]],[[cursor_time]],[[cursor_a]],
			[[cursor_b]],[[cursor_c]],[[cursor_d]],[[cursor_e]],[[deleted_count]],[[started_at]],
			[[updated_at]],[[completed_at]],[[next_due_at]]
		) VALUES ('global',{:status},{:phase},'','','','','','','',0,'',{:now},'','')
	`).WithContext(ctx).Bind(dbx.Params{
		"status": pageAnalyticsRetentionStatusIdle,
		"phase":  pageAnalyticsRetentionPhaseBatches,
		"now":    timestamp,
	}).Execute()
	return storedPageAnalyticsRetentionState{
		Status: pageAnalyticsRetentionStatusIdle, Phase: pageAnalyticsRetentionPhaseBatches,
	}, err
}

func startPageAnalyticsRetentionSweep(
	ctx context.Context,
	app core.App,
	now time.Time,
) (storedPageAnalyticsRetentionState, error) {
	timestamp := pageAnalyticsTimestamp(now)
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_analytics_retention_state}}
		SET [[status]]={:active},[[phase]]={:phase},[[boundary_at]]={:boundary},
			[[cursor_time]]='',[[cursor_a]]='',[[cursor_b]]='',[[cursor_c]]='',[[cursor_d]]='',[[cursor_e]]='',
			[[deleted_count]]=0,[[started_at]]={:now},[[updated_at]]={:now},
			[[completed_at]]='',[[next_due_at]]=''
		WHERE [[id]]='global' AND [[status]]={:idle}
	`).WithContext(ctx).Bind(dbx.Params{
		"active": pageAnalyticsRetentionStatusActive, "idle": pageAnalyticsRetentionStatusIdle,
		"phase": pageAnalyticsRetentionPhaseBatches, "boundary": timestamp, "now": timestamp,
	}).Execute()
	if err != nil {
		return storedPageAnalyticsRetentionState{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return storedPageAnalyticsRetentionState{}, errors.New("page analytics retention sweep did not start")
	}
	return storedPageAnalyticsRetentionState{
		Status: pageAnalyticsRetentionStatusActive, Phase: pageAnalyticsRetentionPhaseBatches,
		BoundaryAt: timestamp,
	}, nil
}

func pageAnalyticsRetentionSpec(phase string, boundary time.Time, shortest int) (pageAnalyticsRetentionPhaseSpec, bool) {
	historyCutoff := pageAnalyticsRetentionCutoff(boundary, 0, shortest)
	dailyCutoff := pageAnalyticsRetentionCutoff(boundary, maxPageAnalyticsReadDays, shortest)
	switch phase {
	case pageAnalyticsRetentionPhaseBatches:
		return pageAnalyticsRetentionPhaseSpec{
			Table: "leafen_page_analytics_batches", DeleteFrom: `DELETE FROM {{leafen_page_analytics_batches}}`,
			Index:       "idx_leafen_page_analytics_batches_retention_global",
			DeleteIndex: "sqlite_autoindex_leafen_page_analytics_batches_1",
			TimeColumn:  "completed_at", Keys: []string{"workspace_id", "batch_id"}, Cutoff: historyCutoff,
			Person: "principal_id",
		}, true
	case pageAnalyticsRetentionPhaseDaily:
		return pageAnalyticsRetentionPhaseSpec{
			Table: "leafen_page_analytics_daily", DeleteFrom: `DELETE FROM {{leafen_page_analytics_daily}}`,
			Index:       "idx_leafen_page_analytics_daily_retention_global",
			DeleteIndex: "sqlite_autoindex_leafen_page_analytics_daily_1",
			TimeColumn:  "retention_at", Keys: []string{"workspace_id", "page_id", "day"},
			Cutoff: dailyCutoff, Shift: maxPageAnalyticsReadDays, Page: "page_id",
		}, true
	case pageAnalyticsRetentionPhaseUnique:
		return pageAnalyticsRetentionPhaseSpec{
			Table: "leafen_page_analytics_unique_viewers", DeleteFrom: `DELETE FROM {{leafen_page_analytics_unique_viewers}}`,
			Index:       "idx_leafen_page_analytics_unique_retention_global",
			DeleteIndex: "sqlite_autoindex_leafen_page_analytics_unique_viewers_1",
			TimeColumn:  "observed_at", Keys: []string{"workspace_id", "page_id", "day", "viewer_kind", "viewer_key"},
			Cutoff: historyCutoff, Page: "page_id", Person: "viewer_key",
		}, true
	case pageAnalyticsRetentionPhaseRecent:
		return pageAnalyticsRetentionPhaseSpec{
			Table: "leafen_page_analytics_recent_viewers", DeleteFrom: `DELETE FROM {{leafen_page_analytics_recent_viewers}}`,
			Index:       "idx_leafen_page_analytics_recent_retention_global",
			DeleteIndex: "sqlite_autoindex_leafen_page_analytics_recent_viewers_1",
			TimeColumn:  "last_viewed_at", Keys: []string{"workspace_id", "page_id", "user_id"}, Cutoff: historyCutoff,
			Page: "page_id", Person: "user_id",
		}, true
	default:
		return rowPageAnalyticsRetentionSpec(phase, historyCutoff, dailyCutoff)
	}
}

func selectPageAnalyticsRetentionCandidates(
	ctx context.Context,
	app core.App,
	state storedPageAnalyticsRetentionState,
	spec pageAnalyticsRetentionPhaseSpec,
) ([]pageAnalyticsRetentionCandidate, bool, error) {
	aliases := []string{"cursor_a", "cursor_b", "cursor_c", "cursor_d", "cursor_e"}
	selected := make([]string, len(aliases))
	for index := range aliases {
		selected[index] = "'' AS [[" + aliases[index] + "]]"
	}
	for index, key := range spec.Keys {
		selected[index] = "[[" + key + "]] AS [[" + aliases[index] + "]]"
	}
	columns := append([]string{"[[" + spec.TimeColumn + "]] AS [[authority_time]]",
		workspaceRetentionDaysSQL("analytics_days", "[["+spec.Table+"]].[[workspace_id]]") + " AS [[days]]",
		legalHoldKeepsSQL(pageAnalyticsRetentionHoldRecord(spec)) + " AS [[held]]"}, selected...)
	order := append([]string{"[[" + spec.TimeColumn + "]]"}, quotedPageAnalyticsRetentionColumns(spec.Keys)...)
	cursorColumns := append([]string{"[[" + spec.TimeColumn + "]]"}, quotedPageAnalyticsRetentionColumns(spec.Keys)...)
	cursorValues := []string{"{:cursorTime}", "{:cursorA}", "{:cursorB}", "{:cursorC}", "{:cursorD}", "{:cursorE}"}
	cursorValues = cursorValues[:len(cursorColumns)]
	rows := []pageAnalyticsRetentionCandidate{}
	err := app.DB().NewQuery(`SELECT ` + strings.Join(columns, ",") +
		` FROM {{` + spec.Table + `}} INDEXED BY [[` + spec.Index + `]]` +
		` WHERE [[` + spec.TimeColumn + `]]<={:cutoff}` +
		` AND ({:cursorTime}='' OR (` + strings.Join(cursorColumns, ",") +
		`)>(` + strings.Join(cursorValues, ",") + `))` +
		` ORDER BY ` + strings.Join(order, ",") + ` LIMIT {:limit}`).
		WithContext(ctx).Bind(dbx.Params{
		"cutoff": spec.Cutoff, "cursorTime": state.CursorTime,
		"cursorA": state.CursorA, "cursorB": state.CursorB,
		"cursorC": state.CursorC, "cursorD": state.CursorD, "cursorE": state.CursorE,
		"limit": pageAnalyticsRetentionBatchSize + 1,
	}).All(&rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > pageAnalyticsRetentionBatchSize
	if hasMore {
		rows = rows[:pageAnalyticsRetentionBatchSize]
	}
	return rows, hasMore, nil
}

// pageAnalyticsRetentionHoldRecord is a row of the phase's table as a legal
// hold reads it.
func pageAnalyticsRetentionHoldRecord(spec pageAnalyticsRetentionPhaseSpec) legalHoldRecordSQL {
	column := func(name string) string { return "[[" + spec.Table + "]].[[" + name + "]]" }
	record := legalHoldRecordSQL{Organization: workspaceOrganizationSQL(column("workspace_id")), Workspace: column("workspace_id")}
	if spec.Page != "" {
		record.Pages = []string{column(spec.Page)}
	}
	if spec.Person != "" {
		record.People = []string{column(spec.Person)}
	}
	return record
}

func deletePageAnalyticsRetentionCandidates(
	ctx context.Context,
	app core.App,
	spec pageAnalyticsRetentionPhaseSpec,
	candidates []pageAnalyticsRetentionCandidate,
) error {
	params := dbx.Params{"cutoff": spec.Cutoff}
	keyColumns := quotedPageAnalyticsRetentionColumns(spec.Keys)
	values := make([]string, len(candidates))
	for rowIndex, candidate := range candidates {
		cursor := []string{candidate.CursorA, candidate.CursorB, candidate.CursorC, candidate.CursorD, candidate.CursorE}
		row := make([]string, len(spec.Keys))
		for keyIndex := range spec.Keys {
			name := fmt.Sprintf("row%dkey%d", rowIndex, keyIndex)
			params[name] = cursor[keyIndex]
			row[keyIndex] = "{:" + name + "}"
		}
		values[rowIndex] = "(" + strings.Join(row, ",") + ")"
	}
	result, err := app.DB().NewQuery(spec.DeleteFrom + ` INDEXED BY [[` + spec.DeleteIndex + `]]` +
		` WHERE (` + strings.Join(keyColumns, ",") + `) IN (` + strings.Join(values, ",") + `)` +
		` AND [[` + spec.TimeColumn + `]]<={:cutoff}`).WithContext(ctx).Bind(params).Execute()
	if err != nil {
		return err
	}
	deleted, err := result.RowsAffected()
	if err != nil || deleted != int64(len(candidates)) {
		return errors.New("page analytics retention lost its selected frontier")
	}
	return nil
}

func updatePageAnalyticsRetentionCursor(
	ctx context.Context,
	app core.App,
	state storedPageAnalyticsRetentionState,
	last pageAnalyticsRetentionCandidate,
	now time.Time,
	deleted int,
) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_analytics_retention_state}}
		SET [[cursor_time]]={:cursorTime},[[cursor_a]]={:cursorA},[[cursor_b]]={:cursorB},
			[[cursor_c]]={:cursorC},[[cursor_d]]={:cursorD},[[cursor_e]]={:cursorE},
			[[deleted_count]]=[[deleted_count]]+{:deleted},[[updated_at]]={:now}
		WHERE [[id]]='global' AND [[status]]={:active} AND [[phase]]={:phase}
			AND [[cursor_time]]={:priorCursorTime}
	`).WithContext(ctx).Bind(dbx.Params{
		"cursorTime": last.AuthorityTime, "cursorA": last.CursorA, "cursorB": last.CursorB,
		"cursorC": last.CursorC, "cursorD": last.CursorD, "cursorE": last.CursorE, "deleted": deleted,
		"now": pageAnalyticsTimestamp(now), "active": pageAnalyticsRetentionStatusActive,
		"phase": state.Phase, "priorCursorTime": state.CursorTime,
	}).Execute()
	return requirePageAnalyticsRetentionStateChange(result, err)
}

func advancePageAnalyticsRetentionPhase(
	ctx context.Context,
	app core.App,
	state storedPageAnalyticsRetentionState,
	nextPhase string,
	now time.Time,
	deleted int,
) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_analytics_retention_state}}
		SET [[phase]]={:nextPhase},[[cursor_time]]='',[[cursor_a]]='',[[cursor_b]]='',
			[[cursor_c]]='',[[cursor_d]]='',[[cursor_e]]='',[[deleted_count]]=[[deleted_count]]+{:deleted},[[updated_at]]={:now}
		WHERE [[id]]='global' AND [[status]]={:active} AND [[phase]]={:phase}
			AND [[cursor_time]]={:priorCursorTime}
	`).WithContext(ctx).Bind(dbx.Params{
		"nextPhase": nextPhase, "deleted": deleted, "now": pageAnalyticsTimestamp(now),
		"active": pageAnalyticsRetentionStatusActive, "phase": state.Phase,
		"priorCursorTime": state.CursorTime,
	}).Execute()
	return requirePageAnalyticsRetentionStateChange(result, err)
}

func completePageAnalyticsRetentionSweep(
	ctx context.Context,
	app core.App,
	state storedPageAnalyticsRetentionState,
	now time.Time,
	deleted int,
	nextDue time.Time,
) error {
	nextDueAt := ""
	if !nextDue.IsZero() {
		nextDueAt = pageAnalyticsTimestamp(nextDue)
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_analytics_retention_state}}
		SET [[status]]={:idle},[[phase]]={:firstPhase},[[boundary_at]]='',
			[[cursor_time]]='',[[cursor_a]]='',[[cursor_b]]='',[[cursor_c]]='',[[cursor_d]]='',[[cursor_e]]='',
			[[deleted_count]]=[[deleted_count]]+{:deleted},[[updated_at]]={:now},
			[[completed_at]]={:now},[[next_due_at]]={:nextDueAt}
		WHERE [[id]]='global' AND [[status]]={:active} AND [[phase]]={:phase}
			AND [[cursor_time]]={:priorCursorTime}
	`).WithContext(ctx).Bind(dbx.Params{
		"idle": pageAnalyticsRetentionStatusIdle, "firstPhase": pageAnalyticsRetentionPhaseBatches,
		"deleted": deleted, "now": pageAnalyticsTimestamp(now), "nextDueAt": nextDueAt,
		"active": pageAnalyticsRetentionStatusActive, "phase": state.Phase,
		"priorCursorTime": state.CursorTime,
	}).Execute()
	return requirePageAnalyticsRetentionStateChange(result, err)
}

func requirePageAnalyticsRetentionStateChange(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return errors.New("page analytics retention state changed concurrently")
	}
	return nil
}

func nextPageAnalyticsRetentionPhase(current string) (string, bool) {
	switch current {
	case pageAnalyticsRetentionPhaseBatches:
		return pageAnalyticsRetentionPhaseDaily, false
	case pageAnalyticsRetentionPhaseDaily:
		return pageAnalyticsRetentionPhaseUnique, false
	case pageAnalyticsRetentionPhaseUnique:
		return pageAnalyticsRetentionPhaseRecent, false
	case pageAnalyticsRetentionPhaseRecent:
		return pageAnalyticsRetentionPhaseRowDaily, false
	default:
		return nextRowPageAnalyticsRetentionPhase(current)
	}
}

// earliestPageAnalyticsRetentionDue is when the next row may be due, by the
// shortest period kept; a row kept longer makes it a time already past.
func earliestPageAnalyticsRetentionDue(ctx context.Context, app core.App, shortest int) (time.Time, error) {
	row := struct {
		Batch     sql.NullString `db:"batch_due"`
		Daily     sql.NullString `db:"daily_due"`
		Unique    sql.NullString `db:"unique_due"`
		Recent    sql.NullString `db:"recent_due"`
		RowDaily  sql.NullString `db:"row_daily_due"`
		RowUnique sql.NullString `db:"row_unique_due"`
		RowRecent sql.NullString `db:"row_recent_due"`
		Members   sql.NullString `db:"member_daily_due"`
		Searches  sql.NullString `db:"search_daily_due"`
		DBDaily   sql.NullString `db:"database_daily_due"`
		DBUnique  sql.NullString `db:"database_unique_due"`
		DBRecent  sql.NullString `db:"database_recent_due"`
	}{}
	err := app.DB().NewQuery(`
		SELECT
			(SELECT MIN([[completed_at]]) FROM {{leafen_page_analytics_batches}}) AS [[batch_due]],
			(SELECT MIN([[retention_at]]) FROM {{leafen_page_analytics_daily}}) AS [[daily_due]],
			(SELECT MIN([[observed_at]]) FROM {{leafen_page_analytics_unique_viewers}}) AS [[unique_due]],
			(SELECT MIN([[last_viewed_at]]) FROM {{leafen_page_analytics_recent_viewers}}) AS [[recent_due]],
			(SELECT MIN([[retention_at]]) FROM {{leafen_row_page_analytics_daily}}) AS [[row_daily_due]],
			(SELECT MIN([[observed_at]]) FROM {{leafen_row_page_analytics_unique_viewers}}) AS [[row_unique_due]],
			(SELECT MIN([[last_viewed_at]]) FROM {{leafen_row_page_analytics_recent_viewers}}) AS [[row_recent_due]],
			(SELECT MIN([[retention_at]]) FROM {{leafen_workspace_analytics_member_daily}}) AS [[member_daily_due]],
			(SELECT MIN([[retention_at]]) FROM {{leafen_workspace_search_daily}}) AS [[search_daily_due]],
			(SELECT MIN([[retention_at]]) FROM {{leafen_database_analytics_daily}}) AS [[database_daily_due]],
			(SELECT MIN([[observed_at]]) FROM {{leafen_database_analytics_unique_viewers}}) AS [[database_unique_due]],
			(SELECT MIN([[last_viewed_at]]) FROM {{leafen_database_analytics_recent_viewers}}) AS [[database_recent_due]]
	`).WithContext(ctx).One(&row)
	if err != nil {
		return time.Time{}, err
	}
	earliest := time.Time{}
	candidates := []sql.NullString{
		row.Batch, row.Daily, row.Unique, row.Recent, row.RowDaily, row.RowUnique, row.RowRecent, row.Members, row.Searches,
		row.DBDaily, row.DBUnique, row.DBRecent,
	}
	for index, candidate := range candidates {
		if !candidate.Valid {
			continue
		}
		due, valid := parsePageAnalyticsRetentionTime(candidate.String)
		if !valid {
			return time.Time{}, fmt.Errorf("page analytics retention timestamp is invalid: %q", candidate.String)
		}
		// Daily rows carry their time a year ahead; the others age out
		// after the period.
		shift := 0
		if index == 1 || index == 4 || index == 7 || index == 8 || index == 9 {
			shift = maxPageAnalyticsReadDays
		}
		due = due.AddDate(0, 0, min(shortest, pageAnalyticsRetentionScanDays)-shift)
		if earliest.IsZero() || due.Before(earliest) {
			earliest = due
		}
	}
	return earliest, nil
}

func parsePageAnalyticsRetentionTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(pageAnalyticsTimeLayout, value)
	return parsed, err == nil
}

func quotedPageAnalyticsRetentionColumns(columns []string) []string {
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = "[[" + column + "]]"
	}
	return quoted
}
