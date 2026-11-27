package app

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/pocketbase/pocketbase/core"
)

// A database's changes, as Notion's get-version-changes lists them (seen
// 2026-11-13): each data source, view and row that changed in the range
// once — a data source as {url, name, schema} with only its changed parts
// when edited, a view as fetch gives it with only its changed settings when
// edited, a row as its values ("properties", its title and data source
// named, its url among them when added or removed, only the changed ones
// when edited, a value it lost null) — and, for a database made in the
// range, its block as its database tag. A row's values are named by its
// data source's properties at the end of the range. Leafen compares the
// database as the page's versions at the two times hold it, and names each
// record's people from the history's changes between them.

// mcpVersionDatabaseSide is a database at one end of a range.
type mcpVersionDatabaseSide struct {
	State  mcpVersionDatabaseState
	Exists bool
	Rows   map[string]bool
}

func readMCPVersionDatabaseSide(
	app core.App, authority mcpAuthority, target mcpHistoryTarget, version *pageHistoryVersionWire, at int64,
) (mcpVersionDatabaseSide, error) {
	side := mcpVersionDatabaseSide{Rows: map[string]bool{}}
	if version == nil {
		return side, nil
	}
	state, err := readMCPVersionDatabase(app, authority, target, version.VersionCursor)
	if err != nil {
		return side, err
	}
	side.State, side.Exists = state, len(state.Sources) > 0 || len(state.Views) > 0
	ids, _, err := mcpVersionRowIDs(app, authority.PrincipalID, state, at)
	for _, id := range ids {
		side.Rows[id] = true
	}
	return side, err
}

// listMCPDatabaseVersionChanges compares a database at a range's start and
// end, as the versions standing then hold it.
func listMCPDatabaseVersionChanges(
	ctx context.Context, app core.App, authority mcpAuthority, target mcpHistoryTarget,
	startVersion *pageHistoryVersionWire, endVersion pageHistoryVersionWire, start, end int64,
	edits map[string]*mcpVersionEdits, inRange []pageHistoryVersionWire,
) ([]notionMCPVersionChange, error) {
	changes := []notionMCPVersionChange{}
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		before, err := readMCPVersionDatabaseSide(txApp, authority, target, startVersion, start)
		if err != nil {
			return err
		}
		after, err := readMCPVersionDatabaseSide(txApp, authority, target, &endVersion, end)
		if err != nil {
			return err
		}
		named, err := mcpVersionDatabaseEdits(txApp, authority, target, edits, inRange)
		if err != nil {
			return err
		}
		edits = maps.Clone(edits)
		maps.Copy(edits, named)
		// What came with the database, made in the range, is its making's
		// too: its data sources, the views made with it and its block.
		if !before.Exists && after.Exists && len(inRange) > 0 {
			// A full-page database is made with its page; an inline one
			// with its block, in its page's history.
			made := inRange[len(inRange)-1]
			creation := &mcpVersionEdits{}
			creation.add(mcpVersionContributor(made), made.CreatedAt)
			if block := edits[target.Database.BlockID]; target.Database.Inline && block != nil && len(block.contributors) > 0 {
				creation = &mcpVersionEdits{}
				creation.add(block.contributors[0], block.first)
			}
			keys := []string{"data_source", target.Database.BlockID}
			for _, source := range after.State.Sources {
				keys = append(keys, "source:"+source.ID)
			}
			for _, key := range keys {
				merged := &mcpVersionEdits{}
				merged.merge(creation)
				merged.merge(edits[key])
				edits[key] = merged
			}
			edits["created"] = creation
		}
		stamp := mcpVersionStamper(edits, inRange)
		sourceChanges, err := mcpVersionSourceChanges(txApp, before.State, after.State, stamp)
		if err != nil {
			return err
		}
		changes = append(changes, sourceChanges...)
		viewChanges, err := mcpVersionViewChanges(txApp, authority, before.State, after.State, stamp)
		if err != nil {
			return err
		}
		changes = append(changes, viewChanges...)
		if !before.Exists && after.Exists {
			source := "" // by its public ID, as fetch's database tag names it
			if len(after.State.Sources) > 0 {
				source = mcpDataSourcePublicID(txApp, after.State.Sources[0].ID)
			}
			tag := notionMarkdownReference{
				Tag: "database", URL: target.Database.URL, DataSourceID: source, Inline: target.Database.Inline,
				Title: mcpDatabaseShownTitle(txApp, authority.PrincipalID, after.State.Name),
			}
			changes = append(changes, stamp(notionMCPVersionChange{Type: "block", Action: "added", After: tag.line()}, target.Database.BlockID))
		}
		rowChanges, err := mcpVersionRowChanges(txApp, authority, target, before, after, start, end, edits, stamp)
		if err != nil {
			return err
		}
		changes = append(changes, rowChanges...)
		return nil
	})
	if errors.Is(err, errMCPToolInvalid) || errors.Is(err, errMCPScopeDenied) {
		return nil, err
	}
	if err != nil {
		return nil, failedBecause(errMCPToolFailed, err)
	}
	return changes, nil
}

// mcpVersionStamper names a record's people and times from the edits
// listed under its keys, the first that has any, else the range's people;
// the first key names the record ("source:", "view:" or "row:" and its
// ID, or a block's ID).
func mcpVersionStamper(edits map[string]*mcpVersionEdits, inRange []pageHistoryVersionWire) func(notionMCPVersionChange, ...string) notionMCPVersionChange {
	whole := &mcpVersionEdits{}
	for _, version := range inRange {
		whole.add(mcpVersionContributor(version), version.CreatedAt)
	}
	return func(change notionMCPVersionChange, keys ...string) notionMCPVersionChange {
		named := whole
		for _, key := range keys {
			if edits[key] != nil {
				named = edits[key]
				break
			}
		}
		change.Contributors = named.contributors
		change.FirstEditTime, change.LastEditTime = mcpVersionTime(named.first), mcpVersionTime(named.last)
		if len(keys) > 0 {
			_, change.record, _ = strings.Cut(keys[0], ":")
			if change.record == "" {
				change.record = keys[0]
			}
		}
		return change
	}
}

// mcpVersionSourceChanges compares a database's data sources: their names,
// and their properties by URL.
func mcpVersionSourceChanges(
	app core.App, before, after mcpVersionDatabaseState, stamp func(notionMCPVersionChange, ...string) notionMCPVersionChange,
) ([]notionMCPVersionChange, error) {
	changes := []notionMCPVersionChange{}
	seen := map[string]bool{}
	for _, source := range append(slices.Clone(after.Sources), before.Sources...) {
		if seen[source.ID] {
			continue
		}
		seen[source.ID] = true
		url := mcpDataSourceURL(app, source.ID)
		was, had := before.source(source.ID)
		now, has := after.source(source.ID)
		change := notionMCPVersionChange{Type: "data_source", URL: url}
		switch {
		case has && !had:
			written, truncated, err := now.sourceWritten(app)
			if err != nil {
				return nil, err
			}
			change.Action, change.After, change.Truncated = "added", written, truncated
		case had && !has:
			written, truncated, err := was.sourceWritten(app)
			if err != nil {
				return nil, err
			}
			change.Action, change.Before, change.Truncated = "removed", written, truncated
		default:
			left, right, err := mcpVersionSourceDifference(app, was, now)
			if err != nil {
				return nil, err
			}
			if len(left) == 0 && len(right) == 0 {
				continue
			}
			change.Action, change.Before, change.After = "edited", left, right
		}
		changes = append(changes, stamp(change, "source:"+source.ID, "data_source"))
	}
	return changes, nil
}

// mcpVersionSourceStateLimit is the longest a data source's state is written
// whole in get-version-changes.
const mcpVersionSourceStateLimit = 2000

// sourceWritten is a data source added or removed as get-version-changes
// writes it, as Notion's does (seen 2026-11-14): its state with the
// properties SQL can't query named before its schema (view-version names
// them after it), unless that, without its url, is longer than 2000
// characters (a formula of 1662 characters was written whole, one of 1671
// not); then its url, name and those properties, marked truncated.
func (source mcpVersionSource) sourceWritten(app core.App) (mcpOrderedObject, bool, error) {
	state, err := source.sourceState(app)
	if err != nil {
		return nil, false, err
	}
	short, schema := mcpOrderedObject{}, mcpOrderedObject{}
	for _, entry := range state {
		if entry.Key == "schema" {
			schema = append(schema, entry)
		} else {
			short = append(short, entry)
		}
	}
	state = append(slices.Clone(short), schema...)
	measured := slices.DeleteFunc(slices.Clone(state), func(entry mcpOrderedEntry) bool { return entry.Key == "url" })
	written, err := mcpFetchJSON(measured)
	if err != nil {
		return nil, false, err
	}
	if len(utf16.Encode([]rune(written))) <= mcpVersionSourceStateLimit {
		return state, false, nil
	}
	return short, true, nil
}

// mcpVersionSourceDifference is what differs between a data source's two
// states: its name, and the properties that changed, came or went.
func mcpVersionSourceDifference(app core.App, was, now mcpVersionSource) (map[string]any, map[string]any, error) {
	left, right := map[string]any{}, map[string]any{}
	if was.Name != now.Name {
		left["name"], right["name"] = was.Name, now.Name
	}
	before, err := was.schema(app)
	if err != nil {
		return nil, nil, err
	}
	after, err := now.schema(app)
	if err != nil {
		return nil, nil, err
	}
	leftSchema, rightSchema := map[string]any{}, map[string]any{}
	for key, entry := range after {
		if previous, had := before[key]; !had || !mcpSameJSON(previous, entry) {
			rightSchema[key] = entry
			if had {
				leftSchema[key] = previous
			}
		}
	}
	for key, entry := range before {
		if _, has := after[key]; !has {
			leftSchema[key] = entry
		}
	}
	if len(leftSchema) > 0 {
		left["schema"] = leftSchema
	}
	if len(rightSchema) > 0 {
		right["schema"] = rightSchema
	}
	return left, right, nil
}

// mcpVersionViewChanges compares a database's views, each as fetch gives
// it, an edited one by the settings it changed.
func mcpVersionViewChanges(
	app core.App, authority mcpAuthority, before, after mcpVersionDatabaseState,
	stamp func(notionMCPVersionChange, ...string) notionMCPVersionChange,
) ([]notionMCPVersionChange, error) {
	changes := []notionMCPVersionChange{}
	written := func(state mcpVersionDatabaseState, id string) (map[string]any, string, bool, error) {
		index := slices.IndexFunc(state.Views, func(view mcpVersionView) bool { return view.ID == id })
		if index < 0 {
			return nil, "", false, nil
		}
		view, err := state.viewState(app, authority, state.Views[index])
		return view, state.Views[index].DataSourceID, true, err
	}
	seen := map[string]bool{}
	for _, view := range append(slices.Clone(after.Views), before.Views...) {
		if seen[view.ID] {
			continue
		}
		seen[view.ID] = true
		was, _, had, err := written(before, view.ID)
		if err != nil {
			return nil, err
		}
		now, sourceID, has, err := written(after, view.ID)
		if err != nil {
			return nil, err
		}
		if !has {
			sourceID = view.DataSourceID
		}
		change := notionMCPVersionChange{Type: "view", URL: mcpViewURLPrefix + view.ID, DataSourceURL: mcpDataSourceURL(app, sourceID)}
		// Its keys in Notion's order, as view-version's (seen 2026-11-14:
		// type, name, dataSourceUrl, displayProperties).
		switch {
		case has && !had:
			change.Action, change.After = "added", mcpVersionViewOrder(now)
		case had && !has:
			change.Action, change.Before = "removed", mcpVersionViewOrder(was)
		default:
			// A view is edited when its own settings changed, not when a
			// property it shows was renamed, as Notion's.
			if mcpVersionViewsSame(before, after, view.ID) {
				continue
			}
			left, right := mcpChangedKeys(was, now)
			if len(left) == 0 && len(right) == 0 {
				continue
			}
			change.Action, change.Before, change.After = "edited", mcpVersionViewOrder(left), mcpVersionViewOrder(right)
		}
		changes = append(changes, stamp(change, "view:"+view.ID, "created", "data_source"))
	}
	return changes, nil
}

// mcpVersionDatabaseWalk is how many of a range's versions that changed a
// database are read to name each data source's and view's own people and
// times; past it, they are named by all those versions together.
const mcpVersionDatabaseWalk = 64

// mcpVersionDatabaseEdits names each data source ("source:<id>") and view
// ("view:<id>") by the versions in the range whose state of it differs from
// the state before, as Notion names each record's own edits.
func mcpVersionDatabaseEdits(
	app core.App, authority mcpAuthority, target mcpHistoryTarget,
	edits map[string]*mcpVersionEdits, inRange []pageHistoryVersionWire,
) (map[string]*mcpVersionEdits, error) {
	ids := map[string]bool{}
	for key, record := range edits {
		if key == "data_source" || strings.HasPrefix(key, "view:") {
			for _, id := range record.versionIDs {
				ids[id] = true
			}
		}
	}
	versions := slices.DeleteFunc(slices.Clone(inRange), func(version pageHistoryVersionWire) bool { return !ids[version.ID] })
	named := map[string]*mcpVersionEdits{}
	if len(versions) == 0 || len(versions) > mcpVersionDatabaseWalk {
		return named, nil
	}
	slices.SortFunc(versions, func(left, right pageHistoryVersionWire) int { return left.VersionCursor - right.VersionCursor })
	// Each version is compared with the one just before it.
	previous, previousCursor := mcpVersionDatabaseState{}, -1
	for _, version := range versions {
		if previousCursor != version.VersionCursor-1 && version.VersionCursor > 0 {
			state, err := readMCPVersionDatabase(app, authority, target, version.VersionCursor-1)
			if err != nil {
				return nil, err
			}
			previous = state
		}
		state, err := readMCPVersionDatabase(app, authority, target, version.VersionCursor)
		if err != nil {
			return nil, err
		}
		for _, key := range mcpVersionDatabaseDifferences(previous, state) {
			if named[key] == nil {
				named[key] = &mcpVersionEdits{}
			}
			named[key].add(mcpVersionContributor(version), version.CreatedAt)
		}
		previous, previousCursor = state, version.VersionCursor
	}
	return named, nil
}

// mcpVersionDatabaseDifferences are the data sources and views that differ
// between two states of a database, as stored.
func mcpVersionDatabaseDifferences(before, after mcpVersionDatabaseState) []string {
	keys := []string{}
	type stored struct {
		Name       string
		Properties []notionMCPDataSourceProperty
	}
	for _, sources := range [][]mcpVersionSource{after.Sources, before.Sources} {
		for _, source := range sources {
			was, had := before.source(source.ID)
			now, has := after.source(source.ID)
			key := "source:" + source.ID
			if !slices.Contains(keys, key) && (had != has || !mcpSameJSON(stored{was.Name, was.Properties}, stored{now.Name, now.Properties})) {
				keys = append(keys, key)
			}
		}
	}
	for _, views := range [][]mcpVersionView{after.Views, before.Views} {
		for _, view := range views {
			key := "view:" + view.ID
			if !slices.Contains(keys, key) && !mcpVersionViewsSame(before, after, view.ID) {
				keys = append(keys, key)
			}
		}
	}
	return keys
}

// mcpVersionViewsSame is whether a view stands the same, as stored, in two
// states of a database.
func mcpVersionViewsSame(before, after mcpVersionDatabaseState, id string) bool {
	find := func(state mcpVersionDatabaseState) (notionMCPDataSourceView, bool) {
		index := slices.IndexFunc(state.Views, func(view mcpVersionView) bool { return view.ID == id })
		if index < 0 {
			return notionMCPDataSourceView{}, false
		}
		return state.Views[index].notionMCPDataSourceView, true
	}
	was, had := find(before)
	now, has := find(after)
	return had == has && mcpSameJSON(was, now)
}

// mcpVersionRowChanges compares the rows the history's changes in the
// range name, each as it was at the start and at the end.
func mcpVersionRowChanges(
	app core.App, authority mcpAuthority, target mcpHistoryTarget, before, after mcpVersionDatabaseSide, start, end int64,
	edits map[string]*mcpVersionEdits, stamp func(notionMCPVersionChange, ...string) notionMCPVersionChange,
) ([]notionMCPVersionChange, error) {
	reader, err := newMCPVersionRowReader(app, authority, target.Page, after.State)
	if err != nil {
		return nil, err
	}
	rowIDs := []string{}
	for key := range edits {
		if id, isRow := strings.CutPrefix(key, "row:"); isRow {
			rowIDs = append(rowIDs, id)
		}
	}
	slices.Sort(rowIDs)
	changes := []notionMCPVersionChange{}
	for _, rowID := range rowIDs {
		had, has := before.Rows[rowID], after.Rows[rowID]
		if !had && !has {
			continue
		}
		source, found := mcpVersionRowSource(app, rowID, after.State, before.State)
		if !found {
			continue
		}
		change := notionMCPVersionChange{Type: "properties", URL: mcpPageURL(authority, rowID), DataSourceURL: mcpDataSourceURL(app, source.ID)}
		var was, now map[string]any
		titleThen, titleNow := "", ""
		if had {
			if was, titleThen, err = reader.values(source, rowID, start); err != nil {
				return nil, err
			}
		}
		if has {
			if now, titleNow, err = reader.values(source, rowID, end); err != nil {
				return nil, err
			}
		}
		// An untitled row is a new page, as Notion's changes name it ("새
		// 페이지", seen 2026-11-14), as fetch does (mcpPageShownTitle).
		titleThen = mcpPageShownTitle(app, authority.PrincipalID, titleThen)
		titleNow = mcpPageShownTitle(app, authority.PrincipalID, titleNow)
		// Its values in its schema's order, an added or removed row's url last, as Notion's (seen
		// 2026-11-14); an edit's leave its automatic ones out (mcpVersionSource.withoutAutomatic).
		switch {
		case has && !had:
			change.Action, change.Title, change.After = "added", titleNow, source.rowValues(now, change.URL)
		case had && !has:
			change.Action, change.Title, change.Before = "removed", titleThen, source.rowValues(was, change.URL)
		default:
			left, right := mcpChangedKeys(source.withoutAutomatic(was), source.withoutAutomatic(now))
			if len(left) == 0 && len(right) == 0 {
				continue
			}
			change.Action, change.Title = "edited", titleNow
			change.Before, change.After = source.valuesInOrder(left), source.valuesInOrder(right)
		}
		changes = append(changes, stamp(change, "row:"+rowID))
	}
	return changes, nil
}

// mcpVersionRowSource is the data source a row is in, as a database's
// later state holds it, else its earlier one.
func mcpVersionRowSource(app core.App, rowID string, states ...mcpVersionDatabaseState) (mcpVersionSource, bool) {
	row, err := loadDatabaseRowPageAuthority(app, rowID)
	if err != nil {
		return mcpVersionSource{}, false
	}
	for _, state := range states {
		if source, found := state.source(row.DataSourceID); found {
			return source, true
		}
	}
	return mcpVersionSource{}, false
}

// mcpChangedKeys are the keys whose values differ between two objects, each
// side's values, null where a side lacks one.
func mcpChangedKeys(before, after map[string]any) (map[string]any, map[string]any) {
	left, right := map[string]any{}, map[string]any{}
	for key, value := range after {
		if previous, had := before[key]; !had || !mcpSameJSON(previous, value) {
			left[key], right[key] = previous, value
		}
	}
	for key, value := range before {
		if _, has := after[key]; !has {
			left[key], right[key] = value, nil
		}
	}
	return left, right
}

func mcpSameJSON(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}
