package app

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace moved from another Leafen server keeps its IDs, so it comes
// only to a server that has none of them: one it was moved to before, or
// imported into keeping them, refuses it before the import writes anything.

const (
	errorCodeWorkspaceMoveAlreadyHere = "LEAFEN_WORKSPACE_MOVE_ALREADY_HERE"
	// errorCodeWorkspaceMoveSameServer refuses a code this server made: the
	// workspace already lives here, so its pages can't come here again.
	errorCodeWorkspaceMoveSameServer = "LEAFEN_WORKSPACE_MOVE_SAME_SERVER"
)

// madeHere reports whether this server made a move code, for one of its own
// workspaces: a workspace here given it would move to where it already is,
// which its kept IDs refuse only once its bundle is sent, so the code's
// check says so at once.
func (in *workspaceMoveIn) madeHere(code serverMoveCode) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	for _, slot := range in.slots {
		if slot.code.ID == code.ID && slot.holdsSecret(code.Secret) {
			return true
		}
	}
	return false
}

// workspaceMoveKeptTables is where each kind the bundle maps lives here.
var workspaceMoveKeptTables = map[string]string{
	workspaceExportItemPage:         "leafen_pages",
	workspaceExportItemRow:          "leafen_pages",
	workspaceExportItemBlock:        "leafen_blocks",
	workspaceExportItemBlockTrash:   "leafen_blocks",
	workspaceExportItemRowPageBlock: "leafen_blocks",
	workspaceExportItemDataSource:   "leafen_data_sources",
	workspaceExportItemView:         "leafen_database_views",
	workspaceExportItemProperty:     "leafen_database_properties",
	workspaceExportItemComment:      "leafen_comments",
	workspaceExportItemTemplate:     "leafen_database_templates",
	workspaceExportItemCustomEmoji:  "leafen_custom_emojis",
	workspaceExportItemTeamspace:    "leafen_teamspaces",
	workspaceImportArchiveFileKind:  "leafen_files",
}

// workspaceMoveIDsFree checks the IDs a moved workspace keeps are free here.
func workspaceMoveIDsFree(app core.App) func(*sql.DB) error {
	return func(mapping *sql.DB) error {
		for kind, table := range workspaceMoveKeptTables {
			rows, err := mapping.Query(`SELECT target_id FROM mappings WHERE kind = ?`, kind)
			if err != nil {
				return workspaceImportStorageError(err)
			}
			batch := []string{}
			check := func() error {
				if len(batch) == 0 {
					return nil
				}
				params, names := dbx.Params{}, make([]string, len(batch))
				for index, id := range batch {
					key := fmt.Sprintf("id%d", index)
					params[key], names[index] = id, "{:"+key+"}"
				}
				taken := 0
				err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{` + table + `}} WHERE [[id]] IN (` + strings.Join(names, ",") + `)`).
					Bind(params).Row(&taken)
				batch = batch[:0]
				if err != nil {
					return err
				}
				if taken > 0 {
					return &workspaceImportFailureError{code: errorCodeWorkspaceMoveAlreadyHere,
						detail: "this server already has a " + kind + " of the workspace: it was moved or imported here before"}
				}
				return nil
			}
			for rows.Next() {
				id := ""
				if err := rows.Scan(&id); err != nil {
					_ = rows.Close()
					return workspaceImportStorageError(err)
				}
				if batch = append(batch, id); len(batch) == 400 {
					if err := check(); err != nil {
						_ = rows.Close()
						return err
					}
				}
			}
			err = rows.Err()
			_ = rows.Close()
			if err != nil {
				return workspaceImportStorageError(err)
			}
			if err := check(); err != nil {
				return err
			}
		}
		return nil
	}
}
