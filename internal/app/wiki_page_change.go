package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A wiki's page changes from its page menu as the wiki database's page it
// is: its title is its title value, its icon its database page's icon, its
// place in the wiki the page it sits under with its place in that page's
// body, and the trash its database page's trash. A page moves within its
// wiki here; it leaves the wiki as a database page leaves its database.

var errWikiPageOutside = graphPreconditionError(
	errorCodeGraphReferenceInvalid, "Move the page out of the wiki from its database page menu.",
)

// wikiPageIconTypes are the icons a database page takes.
var wikiPageIconTypes = map[string]struct{}{"none": {}, "emoji": {}, "image": {}}

func (p *pocketBaseYgoPersistence) changeWikiPageRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input := databaseRowPageWikiChangeInput{}
	if pageID == "" || event.BindBody(&input) != nil || !validWikiPageChange(&input) {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check the page details.")
	}
	response, err := p.changeWikiPage(event.Request.Context(), user.Id, pageID, input)
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "wiki-page")
	}
	return event.JSON(http.StatusOK, response)
}

func validWikiPageChange(input *databaseRowPageWikiChangeInput) bool {
	input.RequestID = strings.TrimSpace(input.RequestID)
	if input.RequestID == "" || utf8.RuneCountInString(input.RequestID) > 1024 {
		return false
	}
	if input.Title != nil && utf8.RuneCountInString(*input.Title) > maxGraphTitleRunes ||
		input.ExpectedTitle != nil && utf8.RuneCountInString(*input.ExpectedTitle) > maxGraphTitleRunes ||
		input.Title == nil && len(input.TitleSpans) > 0 {
		return false
	}
	if (input.Icon == nil) != (input.IconType == nil) {
		return false
	}
	if input.IconType != nil {
		if _, known := wikiPageIconTypes[*input.IconType]; !known || len(*input.Icon) > 4096 {
			return false
		}
	}
	if input.ParentID != nil && len(strings.TrimSpace(*input.ParentID)) > maxGraphIdentifierLength {
		return false
	}
	return true
}

// changeWikiPage applies a page menu change to a wiki's page.
func (p *pocketBaseYgoPersistence) changeWikiPage(
	ctx context.Context, actorID, pageID string, input databaseRowPageWikiChangeInput,
) (databaseRowPageActionResponse, error) {
	preliminary, err := authorizeDatabaseRowPageInTrash(p.app, actorID, pageID, permissionWorkspaceWrite)
	if err != nil {
		return databaseRowPageActionResponse{}, err
	}
	if _, wiki, err := loadWikiSourceProperties(p.app, preliminary.DataSourceID); err != nil || !wiki {
		if err == nil {
			err = errDatabaseRowPageNotFound
		}
		return databaseRowPageActionResponse{}, err
	}
	if input.InTrash != nil && *input.InTrash != (preliminary.TrashedAt != 0) {
		response, err := p.changeDatabaseRowTrash(ctx, actorID, pageID, databaseRowPageActionInput{
			RequestID: input.RequestID + ":trash", ExpectedAuthorityRevision: preliminary.AuthorityRevision,
		}, *input.InTrash)
		if err != nil || input.Title == nil && input.Icon == nil && input.ParentID == nil && input.Position == nil {
			return response, err
		}
	}
	if input.Title == nil && input.Icon == nil && input.ParentID == nil && input.Position == nil {
		return databaseRowPageActionResponse{
			RowID: pageID, ContainerPageID: preliminary.ContainerPageID, AuthorityRevision: preliminary.AuthorityRevision,
		}, nil
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return databaseRowPageActionResponse{}, err
	}
	digest := sha256.Sum256(append([]byte("wiki-page\x00"+pageID+"\x00"), encoded...))
	var current databaseRowPageAuthority
	oldParentID, newParentID := "", ""
	receipt, err := p.applyContentGraphMutationContext(ctx, actorID, preliminary.ContainerPageID,
		graphMutationInput{MutationID: databaseRowPageActionIdentity("wiki-page", actorID, pageID, input.RequestID)},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				authority, err := authorizeDatabaseRowPageInTrash(txApp, actorID, pageID, permissionWorkspaceWrite)
				if err != nil {
					return pageAuthorization{}, err
				}
				if authority.ContainerPageID != preliminary.ContainerPageID {
					return pageAuthorization{}, errDatabaseRowPageConflict
				} else if authority.TrashedAt != 0 {
					return pageAuthorization{}, rowInTrashRefused()
				}
				return authorizePageBody(txApp, actorID, authority.ContainerPageID, permissionWorkspaceWrite)
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				authority, err := authorizeDatabaseRowPageInTrash(txApp, actorID, pageID, permissionWorkspaceWrite)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				current = authority
				row, err := wikiPageRowChange(txApp, authority, input)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				oldParentID, newParentID = authority.ContainerPageID, authority.ContainerPageID
				if authority.SubitemParentID != "" {
					oldParentID = authority.SubitemParentID
				}
				if row.SubitemParentID != nil {
					newParentID = *row.SubitemParentID
				}
				// A mention its writer can't open stays as stored (content_graph_writer_mentions.go).
				written := graphMutationInput{Operations: []graphOperation{{Kind: graphOperationKindUpsertRow, Row: row}}}
				restoreGraphWriteAsStored(txApp, &written)
				return contentGraphMutationPreparation{Operations: written.Operations}, nil
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				if input.Icon != nil {
					if _, err := setRowPageRecordIcon(
						txApp, pageID, current.WorkspaceID, current.ContainerPageID, actorID, *input.Icon, *input.IconType,
					); err != nil {
						return err
					}
				}
				// A place given among the pages beside it is kept; a page come
				// under another page goes last there.
				if input.Position != nil {
					_, err := txApp.DB().NewQuery(`UPDATE {{leafen_pages}} SET [[position]] = {:position} WHERE [[id]] = {:pageId}`).
						Bind(dbx.Params{"position": *input.Position, "pageId": pageID}).Execute()
					return err
				}
				if newParentID == oldParentID {
					return nil
				}
				return placeWikiPageLast(txApp, current.OwnerID, current.WorkspaceID, pageID, current.DataSourceID, newParentID)
			},
		})
	if err != nil {
		return databaseRowPageActionResponse{}, err
	}
	// Its link goes with it to the body of the page it now sits under (wiki_page_links.go).
	if newParentID != oldParentID && !receipt.Duplicate {
		if err := p.keepWikiPageLinks(ctx, actorID, []string{pageID}); err != nil {
			return databaseRowPageActionResponse{}, err
		}
	}
	authority, err := authorizeDatabaseRowPageInTrash(p.app, actorID, pageID, permissionWorkspaceRead)
	if err != nil {
		return databaseRowPageActionResponse{}, err
	}
	return databaseRowPageActionResponse{
		RowID: pageID, ContainerPageID: authority.ContainerPageID,
		AuthorityRevision: authority.AuthorityRevision, Duplicate: receipt.Duplicate,
	}, nil
}

// wikiPageRowChange writes a wiki page's row as the change asks: its title
// value, and the page it sits under — the wiki's page, or another page of the
// wiki that is not the page itself or below it. The row keeps its place in
// the database's order.
func wikiPageRowChange(
	app core.App, authority databaseRowPageAuthority, input databaseRowPageWikiChangeInput,
) (*graphDatabaseRow, error) {
	row := &graphDatabaseRow{
		ID: authority.RowID, DataSourceID: authority.DataSourceID, Position: authority.Position,
		Values: map[string]graphRowValue{},
	}
	if authority.SubitemParentID != "" {
		parentID := authority.SubitemParentID
		row.SubitemParentID = &parentID
	}
	if input.ParentID != nil {
		target := strings.TrimSpace(*input.ParentID)
		switch {
		case target == authority.ContainerPageID:
			row.SubitemParentID = nil
		case target == authority.RowID:
			return nil, errWikiPageOutside
		default:
			var inWiki int
			if err := app.DB().NewQuery(`
				SELECT COUNT(*) FROM {{leafen_database_rows}} AS [[target]]
				WHERE [[target]].[[id]] = {:target} AND [[target]].[[data_source_id]] = {:sourceId}
					AND ` + databaseRowNotTrashedSQL("[[target]].[[id]]") + `
			`).Bind(dbx.Params{"target": target, "sourceId": authority.DataSourceID}).Row(&inWiki); err != nil {
				return nil, err
			}
			if inWiki == 0 {
				return nil, errWikiPageOutside
			}
			if below, err := pageAncestryIncludes(app, target, authority.RowID); err != nil || below {
				if err == nil {
					err = errWikiPageOutside
				}
				return nil, err
			}
			row.SubitemParentID = &target
		}
	}
	// A row is written with a value, so a change of place writes the title
	// the page has, with its mentions and marks.
	var titleID string
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:sourceId} AND [[type]] = 'title' ORDER BY [[position]], [[id]] LIMIT 1
	`).Bind(dbx.Params{"sourceId": authority.DataSourceID}).Row(&titleID); err != nil {
		return nil, err
	}
	stored := struct {
		Value string `db:"value"`
		Spans string `db:"spans"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[value]], [[spans]] FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId}
	`).Bind(dbx.Params{"rowId": authority.RowID, "propertyId": titleID}).One(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	title := graphRowValue{Type: graphValueTypeTitle, Value: json.RawMessage(`""`)}
	if json.Valid([]byte(stored.Value)) {
		title.Value, title.Spans = json.RawMessage(stored.Value), databaseStoredSpans(stored.Spans)
	}
	// A title renamed on the one its writer read is written only over that,
	// as a row page's title is (database_row_values_as_read.go).
	if input.ExpectedTitle != nil {
		read, _ := json.Marshal(*input.ExpectedTitle)
		if !sameRowValueWords(stored.Value, read) {
			return nil, errDatabaseRowPageValueChanged
		}
	}
	if input.Title != nil {
		// Kept as typed, the spaces at its ends too (page_creation.go).
		encoded, err := json.Marshal(*input.Title)
		if err != nil {
			return nil, err
		}
		// Its mentions and marks come with it, as a row page's title's do.
		title = graphRowValue{Type: graphValueTypeTitle, Value: encoded, Spans: input.TitleSpans}
	}
	row.Values[titleID] = title
	return row, nil
}
