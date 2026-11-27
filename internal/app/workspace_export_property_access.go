package app

import (
	"database/sql"
	"errors"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An export holds what the person exporting may see under the databases'
// property access rules (database_property_access.go): a property shown to
// nobody in their place leaves its database, and a value they may not see —
// or a formula or rollup reading one — is left out of each row. A view
// filtering or sorting by such a property shows every row, as one the
// query rules reject does (viewRows).

// applyPropertyAccess takes from the document what its principal may not see.
func (document *workspaceExportDocument) applyPropertyAccess(app core.App) error {
	sources := make([]string, 0, len(document.DataSources))
	for id := range document.DataSources {
		sources = append(sources, id)
	}
	sort.Strings(sources)
	reader, err := newDatabasePropertyAccessReader(app, document.Principal, sources)
	if err != nil || reader == nil {
		return err
	}
	document.Access = reader
	for _, source := range sources {
		restricted := reader.restrictedProperties(source)
		if len(restricted) == 0 {
			continue
		}
		for _, row := range document.Rows[source] {
			values := row.Values
			accessRow := &databasePropertyAccessRow{ID: row.ID, CreatedBy: row.CreatedBy, People: func(propertyID string) []string {
				return peopleInGraphValue(values[propertyID])
			}}
			hidden := []string{}
			for _, propertyID := range restricted {
				level, err := reader.level(source, propertyID, accessRow)
				if err != nil {
					return err
				}
				if !propertyAccessShowsValues(level) {
					hidden = append(hidden, propertyID)
				}
			}
			for _, propertyID := range hidden {
				delete(row.Values, propertyID)
				if row.Hidden == nil {
					row.Hidden = map[string]bool{}
				}
				row.Hidden[propertyID] = true
			}
		}
		kept := document.Properties[source][:0]
		for _, property := range document.Properties[source] {
			if reader.restricted(source, property.ID) {
				shown, err := reader.propertyShown(source, property.ID)
				if err != nil {
					return err
				}
				if !shown {
					continue
				}
			}
			kept = append(kept, property)
		}
		document.Properties[source] = kept
	}
	return nil
}

// withoutHiddenRows takes from the document the unlinked items its
// principal has no role in (database_row_unlinked_access.go), with their
// pages' bodies and comments.
func (document *workspaceExportDocument) withoutHiddenRows(app core.App) error {
	sources := make([]string, 0, len(document.Rows))
	for id := range document.Rows {
		sources = append(sources, id)
	}
	sort.Strings(sources)
	access, err := loadDatabaseRowAccess(app, document.Principal, sources)
	if err != nil || access == nil {
		return err
	}
	for source, rows := range document.Rows {
		kept := rows[:0]
		for _, row := range rows {
			if access.hides(row.ID) {
				delete(document.RowByID, row.ID)
				delete(document.RowBlocks, row.ID)
				delete(document.Comments, row.ID)
				continue
			}
			kept = append(kept, row)
		}
		document.Rows[source] = kept
	}
	return nil
}

// workspaceExportArchiveAccess filters a native archive's items the same
// way, and leaves out every item of an unlinked item its principal has no
// role in: the item, its page's body and document, and its comments.
type workspaceExportArchiveAccess struct {
	app       core.App
	principal string
	readers   map[string]*databasePropertyAccessReader
	// rows: each unlinked item's source's access, by source; hidden: what
	// was decided of each page or item seen.
	rows   map[string]*databaseRowAccess
	hidden map[string]bool
	// closes is the principal's closer for the mentions the archive holds
	// (workspace_export_archive_mentions.go).
	closes func() searchMentionCloser
}

func newWorkspaceExportArchiveAccess(app core.App, principal string) *workspaceExportArchiveAccess {
	return &workspaceExportArchiveAccess{app: app, principal: principal, readers: map[string]*databasePropertyAccessReader{},
		rows: map[string]*databaseRowAccess{}, hidden: map[string]bool{}, closes: readerCloser(app, principal)}
}

// rowHidden is whether the page or item is an unlinked item the principal
// has no role in.
func (access *workspaceExportArchiveAccess) rowHidden(rowID string) (bool, error) {
	if rowID == "" {
		return false, nil
	}
	if hidden, known := access.hidden[rowID]; known {
		return hidden, nil
	}
	var source string
	err := access.app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": rowID}).Row(&source)
	if errors.Is(err, sql.ErrNoRows) {
		access.hidden[rowID] = false
		return false, nil
	}
	if err != nil {
		return false, err
	}
	rows, known := access.rows[source]
	if !known {
		if rows, err = loadDatabaseRowAccess(access.app, access.principal, []string{source}); err != nil {
			return false, err
		}
		access.rows[source] = rows
	}
	access.hidden[rowID] = rows.hides(rowID)
	return access.hidden[rowID], nil
}

func (access *workspaceExportArchiveAccess) reader(dataSourceID string) (*databasePropertyAccessReader, error) {
	if reader, known := access.readers[dataSourceID]; known {
		return reader, nil
	}
	reader, err := newDatabasePropertyAccessReader(access.app, access.principal, []string{dataSourceID})
	if err != nil {
		return nil, err
	}
	access.readers[dataSourceID] = reader
	return reader, nil
}

// keep is whether the archive keeps the item, taking from a row what the
// principal may not see.
func (access *workspaceExportArchiveAccess) keep(item workspaceExportInventoryItem) (bool, error) {
	owner := ""
	switch item.Kind {
	case workspaceExportItemRow:
		owner = mapString(item.Value, "id")
	case workspaceExportItemRowPageBlock, workspaceExportItemRowBlockTrash:
		owner = mapString(item.Value, "rowId")
	case workspaceExportItemYgo, workspaceExportItemComment:
		owner = mapString(item.Value, "pageId")
	}
	if hidden, err := access.rowHidden(owner); err != nil || hidden {
		return false, err
	}
	if item.Kind != workspaceExportItemProperty && item.Kind != workspaceExportItemRow {
		return true, nil
	}
	source := mapString(item.Value, "dataSourceId")
	reader, err := access.reader(source)
	if err != nil || reader == nil {
		return err == nil, err
	}
	if item.Kind == workspaceExportItemProperty {
		propertyID := mapString(item.Value, "id")
		if !reader.restricted(source, propertyID) {
			return true, nil
		}
		return reader.propertyShown(source, propertyID)
	}
	if err := reader.redactRowItem(source, item.Value); err != nil {
		return false, err
	}
	// The archive keeps a row's stored shape; what was hidden is just absent.
	delete(item.Value, "hiddenValues")
	delete(item.Value, "readOnlyValues")
	return true, nil
}

// hides is whether the row's value is one the principal may not see.
func (document *workspaceExportDocument) hides(property *workspaceExportDocProperty, row *workspaceExportDocRow) bool {
	return row.Hidden[property.ID]
}
