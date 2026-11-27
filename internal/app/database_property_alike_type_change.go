package app

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A change between types whose cells are stored alike (text, URL, email and
// phone; select and status) converts nothing: each cell keeps the type it was
// written as, as Notion keeps how a value was written (seen 2026-11-08: text
// made a URL and back was the plain text it was, bold kept). A URL, email or
// phone made text is the one change that rewrites its cells, in the change
// itself: Notion's shows each value linked to the first link it holds
// ("https://…" itself, "example.com/c" with its scheme, "a@example.com" to
// mailto:), so those cells become text cells holding that link. A later
// conversion reads every cell stored alike with the type it converts from.

var databasePropertyLinkedTextTypes = []string{graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone}

// databasePropertyTextLink is where a URL, email or phone value made text
// links, as Notion's change (seen 2026-11-12 on 16 URL, 13 email and 13
// phone values; the web's propertyTextLink): the value's first link as a
// browser reads it (firstTextLink, browserLinkHref), else a "tel:" value as
// written. Notion links any other value to itself, a link that opens nothing;
// such a value stays plain text here, as one that leads nowhere (validGraphTextLink).
func databasePropertyTextLink(value string) string {
	value = strings.TrimSpace(value)
	if link := firstTextLink(value); link != "" {
		return browserLinkHref(link)
	}
	if databasePropertyPhoneScheme.MatchString(value) {
		return value
	}
	return ""
}

var databasePropertyPhoneScheme = regexp.MustCompile(`(?i)^tel:`)

// linkDatabaseTextCells makes the property's cells written as a URL, email
// or phone text cells holding their value linked where it leads; a value
// that leads nowhere stays plain text.
func linkDatabaseTextCells(app core.App, owner, pageID, propertyID string) error {
	types, err := json.Marshal(databasePropertyLinkedTextTypes)
	if err != nil {
		return err
	}
	cells := []databasePropertyConversionCell{}
	if err := app.DB().NewQuery(`SELECT [[row_id]],[[type]],[[value]] FROM {{leafen_database_row_values}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[property_id]]={:propertyId}
			AND [[type]] IN (SELECT [[value]] FROM json_each({:types}))`,
	).Bind(dbx.Params{"owner": owner, "pageId": pageID, "propertyId": propertyID, "types": string(types)}).All(&cells); err != nil {
		return err
	}
	for _, cell := range cells {
		spans := ""
		var text string
		if json.Unmarshal([]byte(cell.Value), &text) == nil {
			if href := databasePropertyTextLink(text); href != "" && validGraphTextLink(href) {
				encoded, err := json.Marshal([]graphTextSpan{{Text: text, Link: href}})
				if err != nil {
					return err
				}
				spans = string(encoded)
			}
		}
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_database_row_values}} SET [[type]]={:type},[[spans]]={:spans}
			WHERE [[row_id]]={:rowId} AND [[property_id]]={:propertyId}`,
		).Bind(dbx.Params{
			"type": graphValueTypeRichText, "spans": spans, "rowId": cell.RowID, "propertyId": propertyID,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// databasePropertyTypesStoredAlike lists the types whose cells read as this
// type's, for a conversion to read every cell a change in place left.
func databasePropertyTypesStoredAlike(valueType string) string {
	alike := []string{}
	for candidate := range graphValueTypes {
		if graphValueTypesShareRepresentation(valueType, candidate) {
			alike = append(alike, candidate)
		}
	}
	encoded, _ := json.Marshal(alike)
	return string(encoded)
}
