package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// databaseRowPageWiki names what a wiki's page shows under its title, as
// Notion's wiki pages (seen 2026-10-15): the wiki's Owner, Verification and
// Tags side by side, its other properties behind "⋯" — its Parent page and
// Sub-pages among them once turned on. A page of any other database has
// none. A wiki's page that is a full-page database shows them over its
// database, its name the page's title, as Notion's (seen 2026-10-29).
func databaseRowPageWiki(
	app core.App, rowID, dataSourceID string, properties []databaseRowPagePropertyWire,
) (*databaseRowPageWikiWire, error) {
	source, found, err := loadWikiSourceProperties(app, dataSourceID)
	if err != nil || !found {
		return nil, err
	}
	listed, tags := map[string]bool{}, ""
	for _, property := range properties {
		listed[property.ID] = true
		if tags == "" && isWikiTagsPropertyID(property.ID) {
			tags = property.ID
		}
	}
	kind, err := storedPageKind(app, rowID)
	if err != nil {
		return nil, err
	}
	wiki := &databaseRowPageWikiWire{
		BarPropertyIDs: []string{}, HiddenPropertyIDs: []string{}, FullPageDatabase: kind == pageKindDatabase,
	}
	for _, id := range []string{source.Owner, source.Verification, tags} {
		if id != "" && listed[id] {
			wiki.BarPropertyIDs = append(wiki.BarPropertyIDs, id)
		}
	}
	return wiki, nil
}
