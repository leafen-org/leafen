package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A template's icon is one bare value, read by its form
// (databaseTemplateRowIcon, the template page triggers
// leafen_database_template_page_insert and _update, and storedIconType on
// the screen): a
// picture's address, a built-in icon's "name:color", a workspace emoji as
// "custom_emoji:<id>" — the form a comment reaction names one by — or an
// emoji. An uploaded picture is kept as its file's address
// (/api/leafen/v1/files/<id>), which reads as a picture; its bare ID, as a
// page keeps one beside its icon's kind, would read as an emoji. A
// workspace emoji stays the emoji, as Notion's template keeps one (seen
// 2026-11-11: the template's menu row, its page and a page made from it all
// show the workspace emoji, and none of them an icon once it is deleted).

// databaseTemplateCustomEmojiPrefix starts a template icon that is a
// workspace emoji, named by its ID.
const databaseTemplateCustomEmojiPrefix = "custom_emoji:"

// databaseTemplateCustomEmojiID is the workspace emoji a template icon names,
// if it names one by its ID as the emoji has it, in lowercase, as the
// template page triggers read it.
func databaseTemplateCustomEmojiID(icon string) (string, bool) {
	id, found := strings.CutPrefix(strings.TrimSpace(icon), databaseTemplateCustomEmojiPrefix)
	return id, found && validCustomEmojiID(id) && id == strings.ToLower(id)
}

// validDatabaseTemplateIcon accepts an icon given to a template: any kind
// its picker sets, a workspace emoji by a well-formed ID.
func validDatabaseTemplateIcon(icon string) bool {
	if len(icon) > maxDatabaseTemplateIconBytes {
		return false
	}
	if strings.HasPrefix(strings.TrimSpace(icon), databaseTemplateCustomEmojiPrefix) {
		_, valid := databaseTemplateCustomEmojiID(icon)
		return valid
	}
	return true
}

// databaseTemplateRowIcon is the icon and its kind a page made from the
// template takes, as the template's own page has them.
func databaseTemplateRowIcon(icon string) (string, string) {
	if id, custom := databaseTemplateCustomEmojiID(icon); custom {
		return id, "custom_emoji"
	}
	return icon, databaseToolbarTemplateRowIconType(icon)
}

// databaseTemplateIconValue is the icon a template keeps for one given to
// it: an uploaded picture given by its file's ID is kept as the file's
// address, anything else as it is.
func databaseTemplateIconValue(icon string) string {
	if fileID, managed := productFilePresentationID(icon); managed {
		return productFilePresentationPrefix + fileID
	}
	return icon
}

// databaseTemplateIconOfPage is the icon a template keeps for a page's icon
// of the given kind: a picture as its address, a workspace emoji as the
// emoji (none once it is gone), no icon as none, any other as it is.
func databaseTemplateIconOfPage(app core.App, workspaceID, icon, iconType string) (string, error) {
	switch iconType {
	case "none":
		return "", nil
	case "image":
		return databaseTemplateIconValue(icon), nil
	case "custom_emoji":
		id := ""
		err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_custom_emojis}}
			WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId}`).
			Bind(dbx.Params{"id": icon, "workspaceId": workspaceID}).Row(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return databaseTemplateCustomEmojiPrefix + id, nil
	}
	return icon, nil
}
