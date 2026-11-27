package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// How a page is verified: the one rule its verification's read and write
// (page_verification.go) and notion-update-page's update_verification
// (mcp_page_verification.go) follow. Notion verifies any page, not only a
// wiki's — "You can verify pages that don't belong to a wiki" (its help on
// wikis and verified pages, read 2026-11-26), and its page head offers
// "인증 추가" on every page. A page held in a
// data source with a Verification property — a wiki's page, or another
// database's page with one — is verified by that property's value, as its
// row; any other page — an ordinary page, a wiki itself, a database opened
// as its own page, a database's page whose data source has no Verification
// property — by its own verification.

type pageVerificationWay struct {
	// DataSourceID is the data source the page is a row of, or "".
	DataSourceID string
	// ByProperty: the page's verification is its data source's Verification
	// property, changed as that value.
	ByProperty bool
	// Wiki: the page is a wiki or one of a wiki's pages, as its fetch says.
	Wiki bool
}

// loadPageVerificationWay reads how a page is verified.
func loadPageVerificationWay(app core.App, pageID string) (pageVerificationWay, error) {
	way := pageVerificationWay{}
	err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&way.DataSourceID)
	if err != nil {
		return way, err
	}
	if way.DataSourceID != "" {
		if _, way.ByProperty, err = loadVerificationSourceProperties(app, way.DataSourceID); err != nil {
			return way, err
		}
	}
	_, err = loadWikiPageMetadata(app, pageID)
	way.Wiki = err == nil
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return way, err
}

// errPageVerifiedByProperty refuses a page's own verification to a page
// whose verification is its database's Verification property.
var errPageVerifiedByProperty = wikiStateRefused("the page's verification is its database's Verification property " +
	"(byProperty): verify it with notion-update-page's update_verification, or set that property's value")
