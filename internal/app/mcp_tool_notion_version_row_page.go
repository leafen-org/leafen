package app

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database row page's own changes, as Notion's get-version-changes lists
// them (seen 2026-11-22, worker w2369, rows an agent made in a synthetic
// database with and without values and edited after): its values as one
// "properties" record, named by its title at the end of the range and by its
// data source, without the url a database's changes give its rows
// (mcpVersionRowChanges) — added, when the page was made in the range, with
// every value it held at the end and then its url; else edited, with only
// the values that changed, a value it lost null, its title among them under
// its column's name. Its people and times are those of the versions that
// changed its values, its making, and the blocks made at, removed from or
// moved among its top, as an ordinary page's title's (a line added later
// was the record's last edit; mcpVersionBlockListEdits).

// mcpVersionPageStateKey names the history's changes to a page's own state
// — its icon, cover and settings, and a database page's values — among its
// edits (mcpVersionEditKey).
const mcpVersionPageStateKey = "page:state"

// mcpVersionRowPage is a database row page's values at a range's two ends,
// named by its data source's properties at the end.
type mcpVersionRowPage struct {
	source        mcpVersionSource
	url, title    string
	before, after map[string]any
}

// readMCPVersionRowPage reads a database row page's values at a range's end
// and, when it stood at the start, at its start; nil for a page that isn't a
// database's row at the end.
func readMCPVersionRowPage(
	ctx context.Context, app core.App, authority mcpAuthority, pageID string, existed bool, start, end time.Time,
) (*mcpVersionRowPage, error) {
	var row *mcpVersionRowPage
	_, err := mcpVersionRowPageAt(ctx, app, authority, pageID, end, func(txApp core.App, source mcpVersionSource, reader mcpVersionRowReader) error {
		now, title, err := reader.values(source, pageID, end.UnixMilli())
		if err != nil {
			return err
		}
		row = &mcpVersionRowPage{
			source: source, url: mcpPageURL(authority, pageID), after: now,
			title: mcpPageShownTitle(txApp, authority.PrincipalID, title),
		}
		if existed {
			row.before, _, err = reader.values(source, pageID, start.UnixMilli())
		}
		return err
	})
	return row, err
}

// properties puts the row page's values' record in place of its title's
// among a page's changes, stamped as the title's edits; none when its values
// didn't change.
func (row *mcpVersionRowPage) properties(
	app core.App, pageID string, created bool, changes []notionMCPVersionChange,
	stamp func(notionMCPVersionChange, ...string) notionMCPVersionChange,
) []notionMCPVersionChange {
	changes = slices.DeleteFunc(changes, func(change notionMCPVersionChange) bool { return change.Type == "properties" })
	change := notionMCPVersionChange{Type: "properties", Title: row.title, DataSourceURL: mcpDataSourceURL(app, row.source.ID)}
	if created {
		change.Action, change.After = "added", row.source.rowValues(row.after, row.url)
	} else {
		left, right := mcpChangedKeys(row.source.withoutAutomatic(row.before), row.source.withoutAutomatic(row.after))
		if len(left) == 0 && len(right) == 0 {
			return changes
		}
		change.Action, change.Before, change.After = "edited", row.source.valuesInOrder(left), row.source.valuesInOrder(right)
	}
	change = stamp(change, "title")
	change.record = pageID
	return append(changes, change)
}

// mcpVersionRowPageEdits counts the versions in a range that changed a
// database row page's values as edits of its title, its values' record:
// each version that changed its state, read against the one before it;
// past mcpVersionDatabaseWalk of them, all those versions.
func mcpVersionRowPageEdits(app core.App, pageID string, edits map[string]*mcpVersionEdits, inRange []pageHistoryVersionWire) error {
	state := edits[mcpVersionPageStateKey]
	if state == nil {
		return nil
	}
	versions := slices.DeleteFunc(slices.Clone(inRange), func(version pageHistoryVersionWire) bool {
		return !slices.Contains(state.versionIDs, version.ID)
	})
	values := state
	if len(versions) <= mcpVersionDatabaseWalk {
		values = &mcpVersionEdits{}
		for _, version := range versions {
			now, err := readPageHistorySnapshotPageState(app, pageID, version.VersionCursor)
			if err != nil {
				return err
			}
			was, err := readPageHistorySnapshotPageState(app, pageID, version.VersionCursor-1)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil && mcpSameJSON(was.Values, now.Values) {
				continue
			}
			values.add(mcpVersionContributor(version), version.CreatedAt)
			values.versionIDs = append(values.versionIDs, version.ID)
		}
	}
	if len(values.contributors) == 0 {
		return nil
	}
	if edits["title"] == nil {
		edits["title"] = &mcpVersionEdits{}
	}
	edits["title"].merge(values)
	for _, id := range values.versionIDs {
		if !slices.Contains(edits["title"].versionIDs, id) {
			edits["title"].versionIDs = append(edits["title"].versionIDs, id)
		}
	}
	return nil
}

// mcpVersionRowPageAt reads a database row page with its data source as its
// database held it at a time and a reader of its values; false for a page
// that isn't a database's row, or a row of no database held at the time.
func mcpVersionRowPageAt(
	ctx context.Context, app core.App, authority mcpAuthority, pageID string, at time.Time,
	read func(txApp core.App, source mcpVersionSource, reader mcpVersionRowReader) error,
) (bool, error) {
	sourceID := ""
	err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&sourceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && mcpOwningDatabaseContainer(app, sourceID) == "" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	target, err := mcpHistoryTargetOf(app, authority, mcpDataSourceURLPrefix+sourceID)
	if err != nil || target.Database == nil {
		return false, err
	}
	versions, err := readMCPHistoryVersions(ctx, target.PageID, at)
	if err != nil || versions.At == nil {
		return false, err
	}
	found := false
	err = runReadSnapshot(ctx, app, func(txApp core.App) error {
		state, err := readMCPVersionDatabase(txApp, authority, target, versions.At.VersionCursor)
		if err != nil {
			return err
		}
		source, held := state.source(sourceID)
		if !held {
			return nil
		}
		reader, err := newMCPVersionRowReader(txApp, authority, target.Page, state)
		if err != nil {
			return err
		}
		found = true
		return read(txApp, source, reader)
	})
	return found, err
}
