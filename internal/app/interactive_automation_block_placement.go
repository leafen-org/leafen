package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type interactiveAutomationInsertion struct{ ParentID, AfterID string }

// Notion's four places resolve inside the same authorized room transaction.
func interactiveAutomationBlockPlacement(app core.App, authorization pageAuthorization, source interactiveAutomationSource, placement string) (interactiveAutomationInsertion, error) {
	result := interactiveAutomationInsertion{ParentID: source.ParentID}
	if placement == "" || placement == "button_below" {
		result.AfterID = source.ID
		return result, nil
	}
	if placement == "page_top" {
		result.ParentID = ""
		return result, nil
	}
	params := dbx.Params{"owner": authorization.Workspace.OwnerID, "page": authorization.Page.Id, "parent": source.ParentID, "source": source.ID}
	query := `SELECT [[id]] FROM {{leafen_blocks}} WHERE [[owner]]={:owner} AND [[page_id]]={:page} AND COALESCE([[parent_id]], '')={:parent}`
	if placement == "page_bottom" {
		result.ParentID = ""
		params["parent"] = ""
	} else if placement == "button_above" {
		query += ` AND ([[position]], [[id]]) < (SELECT [[position]], [[id]] FROM {{leafen_blocks}} WHERE [[owner]]={:owner} AND [[page_id]]={:page} AND [[id]]={:source})`
	} else {
		return result, errInteractiveAutomationInvalid
	}
	var rows []struct {
		ID string `db:"id"`
	}
	if err := app.DB().NewQuery(query + ` ORDER BY [[position]] DESC, [[id]] DESC LIMIT 1`).Bind(params).All(&rows); err != nil {
		return result, err
	}
	if len(rows) > 0 {
		result.AfterID = rows[0].ID
	}
	return result, nil
}
