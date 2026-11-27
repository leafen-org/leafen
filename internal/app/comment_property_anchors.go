package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A comment on a database page's property is a thread on that property's
// value, as Notion's (seen 2026-10-01: the property menu's "댓글" leaves a
// discussion on the value, shown as "<property>: <value>"). As a comment on
// text names its quote, the thread's first comment names the property by
// `propertyId` in its body; replies and edits keep it.

type commentPropertyAnchorBody struct {
	PropertyID json.RawMessage `json:"propertyId"`
}

// commentPropertyAnchor reads the property a comment body names: its id, and
// whether the body names one in a form a thread can start with.
func commentPropertyAnchor(body string) (string, bool) {
	anchor := commentPropertyAnchorBody{}
	if json.Unmarshal([]byte(body), &anchor) != nil || len(anchor.PropertyID) == 0 ||
		string(anchor.PropertyID) == "null" {
		return "", true
	}
	var id string
	if json.Unmarshal(anchor.PropertyID, &id) != nil || !validCommentIdentifier(id) {
		return "", false
	}
	return id, true
}

// validateCommentPropertyAnchor lets a thread start on a property only on a
// database page, not on a block or in reply, and only on a property the
// row's database has and hasn't put in the trash.
func validateCommentPropertyAnchor(
	app core.App, authorization commentAuthorization, input commentCreateInput,
) error {
	propertyID, ok := commentPropertyAnchor(string(input.Body))
	if !ok {
		return errCommentInvalid
	}
	if propertyID == "" {
		return nil
	}
	if authorization.Target.Kind != commentTargetDatabaseRow || input.BlockID != "" || input.ParentID != "" {
		return errCommentInvalid
	}
	var count int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_properties}} AS [[property]]
		JOIN {{leafen_database_rows}} AS [[row]] ON [[row]].[[data_source_id]]=[[property]].[[data_source_id]]
		WHERE [[property]].[[id]]={:propertyId} AND [[row]].[[id]]={:rowId}
			AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
	`).Bind(dbx.Params{"propertyId": propertyID, "rowId": authorization.Target.ID}).Row(&count); err != nil {
		return err
	}
	if count != 1 {
		return errCommentInvalid
	}
	return nil
}

// sameCommentPropertyAnchor keeps an edited comment on the property its
// thread started on.
func sameCommentPropertyAnchor(before, after string) bool {
	previous, _ := commentPropertyAnchor(before)
	next, ok := commentPropertyAnchor(after)
	return ok && previous == next
}
