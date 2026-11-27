package app

import (
	"database/sql"
	"errors"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type wikiPageMetadata struct {
	RootPageID            string `db:"root_page_id"`
	IsWiki                bool   `db:"is_wiki"`
	WikiRevision          int    `db:"wiki_revision"`
	VerifiedAt            string `db:"verified_at"`
	VerifiedBy            string `db:"verified_by"`
	VerificationExpiresAt string `db:"verification_expires_at"`
	// ParentID is the page a page of a wiki's database sits under.
	ParentID string `db:"parent_id"`
	// DataSourceID is the wiki's data source: the one a wiki's page holds,
	// or the one a page of the wiki belongs to.
	DataSourceID string `db:"data_source_id"`
}

func loadWikiPageMetadata(app core.App, pageID string) (wikiPageMetadata, error) {
	metadataByPage, err := loadWikiPageMetadataSet(app, []string{pageID})
	if err != nil {
		return wikiPageMetadata{}, err
	}
	metadata, exists := metadataByPage[pageID]
	if !exists {
		return wikiPageMetadata{}, sql.ErrNoRows
	}
	return metadata, nil
}

func loadWikiPageMetadataSet(app core.App, pageIDs []string) (map[string]wikiPageMetadata, error) {
	result := map[string]wikiPageMetadata{}
	if len(pageIDs) == 0 {
		return result, nil
	}
	params := dbx.Params{}
	placeholders := make([]string, 0, len(pageIDs))
	for index, pageID := range pageIDs {
		key := "page" + strconv.Itoa(index)
		params[key] = pageID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []struct {
		PageID string `db:"page_id"`
		wikiPageMetadata
	}{}
	// A wiki's pages are its database's pages: the wiki's page, and each of
	// its pages under the page it sits under.
	err := app.DB().NewQuery(`
		SELECT [[page]].[[id]] AS [[page_id]], COALESCE([[source]].[[page_id]], [[page]].[[id]]) AS [[root_page_id]],
			CASE WHEN [[source]].[[id]] IS NULL THEN 1 ELSE 0 END AS [[is_wiki]],
			[[page]].[[wiki_revision]],[[page]].[[verified_at]],[[page]].[[verified_by]],
			[[page]].[[verification_expires_at]],
			CASE WHEN [[source]].[[id]] IS NULL THEN '' ELSE COALESCE(` + pageWikiParentSQL("[[page]]") + `, '') END AS [[parent_id]],
			COALESCE([[source]].[[id]], (SELECT [[held]].[[id]] FROM {{leafen_data_sources}} [[held]]
				WHERE [[held]].[[page_id]]=[[page]].[[id]] AND [[held]].[[wiki]]=1 ORDER BY [[held]].[[id]] LIMIT 1), '') AS [[data_source_id]]
		FROM {{leafen_pages}} [[page]]
		LEFT JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]]=[[page]].[[data_source_id]] AND [[source]].[[wiki]]=1
		WHERE [[page]].[[id]] IN (` + joinSQL(placeholders) + `) AND ([[source]].[[id]] IS NOT NULL OR EXISTS (
			SELECT 1 FROM {{leafen_data_sources}} [[held]] WHERE [[held]].[[page_id]]=[[page]].[[id]] AND [[held]].[[wiki]]=1))
	`).Bind(params).All(&rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.PageID] = row.wikiPageMetadata
	}
	return result, nil
}

func pageRecordResponseWithWiki(app core.App, record *core.Record) (pageResponse, error) {
	response := pageRecordResponse(record)
	if record.GetBool("in_trash") {
		signpost, err := isDatabasePageSignpost(app, record.Id)
		if err != nil {
			return pageResponse{}, err
		}
		response.MovedDatabase = signpost
	}
	template, err := isButtonTemplatePage(app, record.Id)
	if err != nil {
		return pageResponse{}, err
	}
	response.ButtonTemplate = template
	response.AnchorUnmentioned = record.GetBool(pageAnchorUnmentionedField)
	if response.FieldAnchored, err = isFieldAnchoredPage(app, record); err != nil {
		return pageResponse{}, err
	}
	if isFullPageDatabase(record) {
		if response.ViewBlock, err = isViewBlockDatabasePage(app, record.Id); err != nil {
			return pageResponse{}, err
		}
	}
	metadata, err := loadWikiPageMetadata(app, record.Id)
	if errors.Is(err, sql.ErrNoRows) {
		return pageResponseAsItsRow(app, response), nil
	}
	if err != nil {
		return pageResponse{}, err
	}
	return pageResponseAsItsRow(app, applyWikiMetadataToPageResponse(response, metadata)), nil
}

func applyWikiMetadataToPageResponse(response pageResponse, metadata wikiPageMetadata) pageResponse {
	response.IsWiki = metadata.IsWiki
	response.WikiRootID = metadata.RootPageID
	response.VerifiedAt = metadata.VerifiedAt
	response.VerifiedBy = metadata.VerifiedBy
	response.VerificationExpiresAt = metadata.VerificationExpiresAt
	response.WikiRevision = metadata.WikiRevision
	if metadata.ParentID != "" {
		response.ParentID = metadata.ParentID
	}
	response.WikiDataSourceID = metadata.DataSourceID
	response.DatabaseRow = response.DatabaseRow && metadata.DataSourceID == ""
	return response
}
