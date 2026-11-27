package app

import (
	"database/sql"
	"errors"
	"reflect"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A version's change to a database the page holds, as Notion's "버전의 모든
// 변경 사항 보기" names it (seen 2026-10-01, a temporary page's inline
// database): a renamed property is "속성을 변경함" with "Old → New" under it,
// and a view made is "Probe List 보기 생성함" with its kind under it, with
// "데이터베이스 블록 블록을 편집했습니다" for its block at the same time. Any
// other change to the data source, its properties or its views stays
// "데이터베이스 편집함", one for the data source.

const (
	pageHistoryChangePropertyChanged = "propertyChanged"
	pageHistoryChangeViewCreated     = "viewCreated"
)

// pageHistoryDatabaseChangeCards names what a version changed in one data
// source from its state before, held or not, to its state after, nil when
// the page no longer holds it.
func pageHistoryDatabaseChangeCards(
	before pageHistoryDatabaseState, held bool, after *pageHistoryDatabaseState,
) []pageHistoryChangeWire {
	if !held || after == nil {
		return []pageHistoryChangeWire{{Kind: pageHistoryChangeDatabaseEdited}}
	}
	cards := []pageHistoryChangeWire{}
	other := !reflect.DeepEqual(pageHistoryComparableSource(before.Source), pageHistoryComparableSource(after.Source))
	properties := make(map[string]pageHistoryDatabaseProperty, len(before.Properties))
	for _, property := range before.Properties {
		properties[property.ID] = property
	}
	for _, property := range after.Properties {
		previous, found := properties[property.ID]
		delete(properties, property.ID)
		switch {
		case !found:
			other = true
		case previous.Name != property.Name:
			cards = append(cards, pageHistoryChangeWire{
				Kind: pageHistoryChangePropertyChanged, ItemID: property.ID, FromName: previous.Name, ToName: property.Name,
			})
			other = other || previous.Type != property.Type ||
				!pageHistorySamePropertyConfig(string(previous.Config), string(property.Config))
		case previous.Type != property.Type || previous.Position != property.Position ||
			!pageHistorySamePropertyConfig(string(previous.Config), string(property.Config)):
			other = true
		}
	}
	other = other || len(properties) > 0
	views := make(map[string]pageHistoryDatabaseView, len(before.Views))
	for _, view := range before.Views {
		views[view.ID] = view
	}
	for _, view := range after.Views {
		previous, found := views[view.ID]
		delete(views, view.ID)
		if !found {
			cards = append(cards, pageHistoryChangeWire{
				Kind: pageHistoryChangeViewCreated, ItemID: view.ID, ViewName: view.Name, ViewType: view.Type,
			})
			continue
		}
		if previous.Name != view.Name || previous.Type != view.Type || previous.Position != view.Position ||
			previous.ContainerID != view.ContainerID || !pageHistorySameJSON(string(previous.Config), string(view.Config)) {
			other = true
		}
	}
	other = other || len(views) > 0
	if other || len(cards) == 0 {
		cards = append(cards, pageHistoryChangeWire{Kind: pageHistoryChangeDatabaseEdited})
	}
	return cards
}

// pageHistoryComparableSource is a data source's fields as they show: a
// cover's position counts only with a cover.
func pageHistoryComparableSource(source *pageHistoryDatabaseSource) *pageHistoryDatabaseSource {
	if source == nil {
		return nil
	}
	comparable := *source
	if comparable.Cover == "" {
		comparable.CoverPosition = 0
	}
	return &comparable
}

// pageHistoryDatabaseBlock is the page's block that shows a data source, if
// any.
func pageHistoryDatabaseBlock(app core.App, pageID, sourceID string) (struct{ ID, Type string }, error) {
	block := struct {
		ID   string `db:"id"`
		Type string `db:"type"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[type]] FROM {{leafen_blocks}}
		WHERE [[page_id]]={:pageId} AND json_valid([[content]]) AND json_extract([[content]],'$.dataSourceId')={:sourceId}
		ORDER BY [[id]] LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "sourceId": sourceID}).One(&block)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return struct{ ID, Type string }{block.ID, block.Type}, err
}
