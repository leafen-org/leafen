package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where an update's thing is, under its line as Notion's workspace updates
// say it (seen 2026-11-13): what stands above it from the top down — the
// pages, a database page's database and, while that holds several sources,
// its source — read as the path above a page is (page_path_route.go), so it
// stops at the first piece its reader can't read and says nothing of those
// above it. A page's line says what stands above the page, a block's the
// page it is on and what stands above that, a view's where its database
// stands: the page holding an inline one, what stands above a full-page
// one. A database's own line and a property's say none. A page in a
// teamspace has its teamspace first, by name and no link: "· 〈teamspace〉"
// for a line of the teamspace's top page, "· 〈teamspace〉 / 〈page〉" one
// down (seen 2026-11-16 once and 2026-11-17 in another tab, page, block and
// permission lines of three teamspaces; the name in the secondary color with
// the plain pointer, the pages links). The app draws the first piece and the
// last with "..." between (WorkspaceActivityFeed.tsx).

// workspaceActivityPlaces keeps the places one turn of the feed has read, by
// what they are read from: lines in one page say where it is once.
type workspaceActivityPlaces map[string][]workspaceActivityPlace

func (places workspaceActivityPlaces) read(ctx context.Context, app core.App, actor, entityType string, holder, subject *core.Record) ([]workspaceActivityPlace, error) {
	page, withPage := subject, false
	switch entityType {
	case "page":
	case "block":
		page, withPage = holder, true
	case "view":
		page, withPage = holder, holder.GetString("kind") != pageKindDatabase
	default:
		return nil, nil
	}
	key := page.Id
	if withPage {
		key = "with:" + key
	}
	if known, found := places[key]; found {
		return known, nil
	}
	pieces, err := pagePathAboveFor(ctx, app, actor, page)
	if err != nil {
		return nil, err
	}
	if teamspace, err := places.teamspace(app, page.GetString("teamspace_id")); err != nil {
		return nil, err
	} else if teamspace != nil {
		pieces = append([]pagePathPieceWire{*teamspace}, pieces...)
	}
	if withPage {
		title := pageTitleAsReadBy(app, readerCloser(app, actor), page.Id, page.GetString("title"))
		piece := pagePathPieceWire{Kind: pagePathPiecePage, ID: page.Id, PageID: page.Id, Title: title}
		if strings.TrimSpace(title) == "" && isDatabaseRowPageRecord(page) {
			if piece.UntitledName, err = pagePathUntitledName(ctx, app, actor, page); err != nil {
				return nil, err
			}
		}
		pieces = append(pieces, piece)
	}
	// A database's page without a title goes by what its path calls it (pagePathUntitledName).
	place := make([]workspaceActivityPlace, 0, len(pieces))
	for _, piece := range pieces {
		place = append(place, workspaceActivityPlace{Kind: piece.Kind, ID: piece.ID, PageID: piece.PageID, BlockID: piece.BlockID,
			ViewID: piece.ViewID, Title: piece.Title, SourceTitles: piece.SourceTitles, UntitledName: piece.UntitledName})
	}
	places[key] = place
	return place, nil
}

// teamspace is the piece naming a page's teamspace, nil for a page in none
// or a teamspace gone; one turn reads each teamspace once.
func (places workspaceActivityPlaces) teamspace(app core.App, teamspaceID string) (*pagePathPieceWire, error) {
	if teamspaceID == "" {
		return nil, nil
	}
	key := "teamspace:" + teamspaceID
	if known, found := places[key]; found {
		if len(known) == 0 {
			return nil, nil
		}
		return &pagePathPieceWire{Kind: known[0].Kind, ID: known[0].ID, Title: known[0].Title}, nil
	}
	var name string
	err := app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_teamspaces}} WHERE [[id]]={:id} AND [[deleted_at]]=0`).
		Bind(dbx.Params{"id": teamspaceID}).Row(&name)
	if errors.Is(err, sql.ErrNoRows) {
		places[key] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	places[key] = []workspaceActivityPlace{{Kind: workspaceActivityPlaceTeamspace, ID: teamspaceID, Title: name}}
	return &pagePathPieceWire{Kind: workspaceActivityPlaceTeamspace, ID: teamspaceID, Title: name}, nil
}

// workspaceActivityPlaceTeamspace is the kind of a place's teamspace piece.
const workspaceActivityPlaceTeamspace = "teamspace"
