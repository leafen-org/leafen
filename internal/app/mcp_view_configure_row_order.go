package app

import (
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
)

// ROW ORDER "row", "row", … puts a view's rows in the order a person drags
// them into on the screen (the view's `rowOrder`, viewRowOrder.ts): those
// named first, in that order, and the rest after them, newest first, as the
// screen shows rows it has not placed. Each row is its page's ID or address.
// It is how an agent matches a Notion view's rows to the order Notion's
// screen shows, which Notion's API doesn't give (formats_left's row_order,
// notion_migration_guide.md). A view with sorts shows its rows by them, and
// the order only breaks their ties.

// rowOrder reads ROW ORDER's rows, after the words.
func (change *mcpViewConfigure) rowOrder(words *mcpViewWords) error {
	form := `ROW ORDER "row page ID", "row page ID"`
	ids := []string{}
	source := ""
	for {
		named, ok := words.quoted()
		if !ok {
			return change.unreadable("ROW ORDER", form)
		}
		id, rowSource, err := mcpViewRowOrderRow(change, named)
		if err != nil {
			return err
		}
		if source != "" && rowSource != source {
			return mcpInvalid("configure: ROW ORDER names rows of one database; %q is another's", named)
		}
		source = rowSource
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
		if !words.mark(",") {
			break
		}
	}
	change.config["rowOrder"] = mcpAnyStrings(ids)
	return nil
}

// mcpViewRowOrderRow is the row page a ROW ORDER entry names, by its ID or
// its address, and its data source; anything else is refused by name.
func mcpViewRowOrderRow(change *mcpViewConfigure, named string) (string, string, error) {
	id := strings.TrimSpace(named)
	if internal, _ := mcpInternalPageID(id); internal != "" {
		id = internal
	} else if public := notionLinkedID(id); public != "" {
		if own, err := resolvePageOwnPublicID(public); err == nil {
			id = own
		}
	}
	var row struct {
		DataSourceID string `db:"data_source_id"`
		InTrash      bool   `db:"in_trash"`
	}
	err := change.app.DB().NewQuery(`SELECT [[data_source_id]], [[in_trash]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": id}).One(&row)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (row.DataSourceID == "" || row.InTrash) {
		return "", "", mcpInvalid("configure: ROW ORDER names rows by their page IDs; %q isn't a database row", named)
	}
	if err != nil {
		return "", "", err
	}
	return id, row.DataSourceID, nil
}
