package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where a backlink is, as Notion's names it (seen 2026-11-22 on synthetic
// pages: a link-to-page block and an @-mention in a paragraph, a database
// page's body among them, each a block_id; a mention in a page's title none;
// one in a database page's property — a relation — the property's ID, as
// its schema has it and not encoded, and name, {"id":"{\\ZW","name":"프로젝트"},
// and no block_id). Leafen's backlinks name a title by "title:" and the page
// and a cell by "cell:", the row and the property (page_field_references_schema.go),
// keys of its own no tool reads.

type notionMCPBacklinkProperty struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// mcpBacklinkKind is a backlink's kind by where it is: a link-to-page
// block, a database page's property, or any other text that mentions the
// page — a block's, a database page's body's among them, or a title.
func mcpBacklinkKind(block pageBacklinksBlockWire) string {
	switch block.Type {
	case graphBlockTypeLinkToPage:
		return "link_to_page"
	case pageFieldReferenceCellType:
		return "property_mention"
	}
	return "page_mention"
}

// mcpBacklinkPlaces names each backlink's place as Notion's: a block by its
// ID, a property by its ID and name, a title by nothing.
func mcpBacklinkPlaces(app core.App, backlinks []notionMCPBacklink) error {
	properties := []string{}
	for _, backlink := range backlinks {
		if property := mcpBacklinkCellProperty(backlink.BlockID); property != "" {
			properties = append(properties, property)
		}
	}
	public, names := map[string]string{}, map[string]string{}
	if len(properties) > 0 {
		var err error
		if public, err = notionPublicPropertyIDsOf(app, properties); err != nil {
			return err
		}
		if names, err = mcpDatabasePropertyNames(app, properties); err != nil {
			return err
		}
	}
	for index := range backlinks {
		backlink := &backlinks[index]
		switch property := mcpBacklinkCellProperty(backlink.BlockID); {
		case property != "":
			backlink.BlockID = ""
			backlink.Property = &notionMCPBacklinkProperty{ID: notionPropertyRawID(public[property]), Name: names[property]}
		case strings.HasPrefix(backlink.BlockID, pageFieldReferenceTitlePrefix):
			backlink.BlockID = ""
		}
	}
	return nil
}

// mcpBacklinkCellProperty is the property of a cell's key ("cell:", the
// row and the property); "" for any other.
func mcpBacklinkCellProperty(key string) string {
	cell, isCell := strings.CutPrefix(key, pageFieldReferenceCellPrefix)
	if !isCell {
		return ""
	}
	_, property, _ := strings.Cut(cell, ":")
	return property
}

// mcpDatabasePropertyNames are properties' names by their IDs.
func mcpDatabasePropertyNames(app core.App, ids []string) (map[string]string, error) {
	wanted := make([]any, 0, len(ids))
	for _, id := range ids {
		wanted = append(wanted, id)
	}
	rows := []struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}{}
	if err := app.DB().Select("id", "name").From("leafen_database_properties").Where(dbx.In("id", wanted...)).All(&rows); err != nil {
		return nil, err
	}
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	return names, nil
}
