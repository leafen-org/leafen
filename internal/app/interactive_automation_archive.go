package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A run's "페이지 보관하기…", as Notion's (seen 2026-11-15 in a database's
// automation and a page's button): the pages its target names — the page
// that set it off, the row whose button was pressed, or a database's pages
// its filter matches — are archived, each by its own archive state
// (database_row_page_archive.go). An archived page leaves its database's
// views without a word and is listed under "보관된 페이지 보기".

// plannedInteractiveAutomationArchive is a row a run archives, with the page
// holding its database.
type plannedInteractiveAutomationArchive struct {
	RowID      string
	DatabaseID string
	PageID     string
}

// aimAtPages plans a step aimed at pages: an edit of their values, their
// archive or their icon (interactive_automation_page_icon.go).
func (plan *interactiveAutomationPlan) aimAtPages(
	app core.App,
	authorization pageAuthorization,
	source interactiveAutomationSource,
	action interactiveAutomationAction,
	properties map[string]interactiveAutomationProperty,
	variables map[string]json.RawMessage,
	triggerValues map[string]json.RawMessage,
	executionTime time.Time,
) error {
	if action.Type == interactiveAutomationActionArchivePages {
		return plan.archivePages(app, authorization, source, action)
	}
	if action.Type == interactiveAutomationActionSetPageIcon {
		return plan.setPageIcon(app, authorization, source, action)
	}
	if interactiveAutomationTargetsThisPage(action) {
		return plan.editThisPage(source, action, properties, variables, triggerValues, executionTime)
	}
	return plan.editPages(app, authorization, action, properties, variables, triggerValues, executionTime)
}

// archivePages plans the archive of the pages the step names: the trigger's
// row for this_page, or the rows a filter turn chose (targetRows).
func (plan *interactiveAutomationPlan) archivePages(
	app core.App, authorization pageAuthorization, source interactiveAutomationSource, action interactiveAutomationAction,
) error {
	if !interactiveAutomationTargetsThisPage(action) {
		databaseID, ids, err := plan.targetRows(app, authorization, action)
		if err != nil {
			return err
		}
		for _, id := range ids {
			plan.archive(id, databaseID)
		}
		return nil
	}
	if source.TriggerRowID == "" {
		return errAutomationNoTriggerRow
	}
	// The page that set off a run may have gone to the trash since: then
	// there is nothing to archive, as with a page it would edit.
	if err := plan.rowsExist(app, authorization, source.TriggerDatabaseID, []string{source.TriggerRowID}); err != nil {
		return err
	}
	plan.archive(source.TriggerRowID, source.TriggerDatabaseID)
	return nil
}

func (plan *interactiveAutomationPlan) archive(rowID, databaseID string) {
	for _, planned := range plan.Archives {
		if planned.RowID == rowID {
			return
		}
	}
	plan.Archives = append(plan.Archives, plannedInteractiveAutomationArchive{
		RowID: rowID, DatabaseID: databaseID, PageID: plan.pageOf(databaseID),
	})
}

// targetRows are the rows of a database a step edits or archives, as a
// filter turn names them (prepareInteractiveAutomationFilterTurn), or a
// list kept from before filters: each once, none in the trash.
func (plan *interactiveAutomationPlan) targetRows(
	app core.App, authorization pageAuthorization, action interactiveAutomationAction,
) (string, []string, error) {
	var target struct {
		DatabaseID string `json:"databaseId"`
		Filter     struct {
			RowIDs []string `json:"rowIds"`
		} `json:"filter"`
		Limit int `json:"limit"`
	}
	if json.Unmarshal(action.Target, &target) != nil || len(target.Filter.RowIDs) < 1 ||
		len(target.Filter.RowIDs) > target.Limit || len(target.Filter.RowIDs) > interactiveAutomationMaxTargetRows {
		return "", nil, errInteractiveAutomationUnsupported
	}
	ids := append([]string(nil), target.Filter.RowIDs...)
	for index := range ids {
		ids[index] = strings.TrimSpace(ids[index])
		if !boundedAutomationString(ids[index], interactiveAutomationMaxIdentifierRunes) {
			return "", nil, errInteractiveAutomationInvalid
		}
	}
	sort.Strings(ids)
	for index := 1; index < len(ids); index++ {
		if ids[index] == ids[index-1] {
			return "", nil, errInteractiveAutomationInvalid
		}
	}
	return target.DatabaseID, ids, plan.rowsExist(app, authorization, target.DatabaseID, ids)
}

// rowsExist checks that each row is the database's and not in the trash.
func (plan *interactiveAutomationPlan) rowsExist(app core.App, authorization pageAuthorization, databaseID string, ids []string) error {
	params := dbx.Params{"owner": authorization.Workspace.OwnerID, "pageId": plan.pageOf(databaseID), "databaseId": databaseID}
	bindings := make([]string, len(ids))
	for index, id := range ids {
		key := fmt.Sprintf("row%d", index)
		params[key] = id
		bindings[index] = "{:" + key + "}"
	}
	var count int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_rows}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:databaseId}
			AND [[id]] IN (` + strings.Join(bindings, ",") + `)
			AND ` + databaseRowNotTrashedSQL("[[id]]")).Bind(params).Row(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return errInteractiveAutomationNotFound
	}
	return nil
}

// applyInteractiveAutomationArchives archives the run's rows, a page already
// archived staying as it was, and gives back those it archived; a database's
// automation archives them as Leafen's, as it writes. Each archived row's
// database reads it again where it is open.
func applyInteractiveAutomationArchives(
	app core.App, authorization pageAuthorization, plan interactiveAutomationPlan,
) ([]plannedInteractiveAutomationArchive, error) {
	if len(plan.Archives) == 0 {
		return nil, nil
	}
	actor := authorization.Workspace.PrincipalID
	if plan.SystemAuthored {
		actor = leafenSystemAuthorID
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	archived := make([]plannedInteractiveAutomationArchive, 0, len(plan.Archives))
	for _, archive := range plan.Archives {
		changed, err := applyDatabaseRowPageArchiveState(
			app, archive.RowID, authorization.Workspace.WorkspaceID, archive.PageID, actor, true, at,
		)
		if err != nil {
			return nil, err
		}
		if !changed {
			continue
		}
		if err := announceDatabaseRowPageChange(app, archive.RowID); err != nil {
			return nil, err
		}
		archived = append(archived, archive)
	}
	return archived, nil
}

// noteInteractiveAutomationUndoArchives keeps the rows a button's run
// archived, for its notice's "실행 취소".
func noteInteractiveAutomationUndoArchives(progress *interactiveAutomationProgress, archived []plannedInteractiveAutomationArchive) {
	for _, archive := range archived {
		if progress.UndoArchives == nil {
			progress.UndoArchives = map[string]string{}
		}
		progress.UndoArchives[archive.RowID] = archive.PageID
	}
}

// restoreInteractiveAutomationArchives takes back the archives of a button's
// run: each row it archived that is archived still comes back to its views.
// Notion's notice offers "실행 취소" after an archive too (seen 2026-11-15),
// though the row stayed archived there once.
func restoreInteractiveAutomationArchives(app core.App, authorization pageAuthorization, progress *interactiveAutomationProgress) error {
	if progress == nil || len(progress.UndoArchives) == 0 {
		return nil
	}
	ids := make([]string, 0, len(progress.UndoArchives))
	for id := range progress.UndoArchives {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	at := time.Now().UTC().Format(time.RFC3339Nano)
	for _, id := range ids {
		changed, err := applyDatabaseRowPageArchiveState(
			app, id, authorization.Workspace.WorkspaceID, progress.UndoArchives[id], authorization.Workspace.PrincipalID, false, at,
		)
		if err != nil {
			return err
		}
		if changed {
			if err := announceDatabaseRowPageChange(app, id); err != nil {
				return err
			}
		}
	}
	return nil
}
