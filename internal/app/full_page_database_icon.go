package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A full-page database of one data source of its own shows that source's
// icon, as Notion's does (seen 2026-11-22 through the public API and in
// Chrome at 1440: the source's icon set alone draws its page's head, its
// mention, its sidebar row and its path; its page's head icon picked sets
// the database's and the source's). Its page keeps the icon those draw, so
// the two are kept one: a change that writes the source's icon gives its
// page the same (carrySourceIconToPage), its page's icon changed gives its
// source the same (nameFullPageDatabase). The source's set alone, the
// database keeps the icon it had apart, as Notion's API reads it, and its
// page shows that again once a second source comes (seen the same day: the
// database given 🍋, its source 🍎 alone — the head 🍎 —, then a second
// source — the head 🍋 over the source's heading 🍎). Its block keeps that
// icon (ownIcon) beside the one its page took (ownIconFor), as it keeps a
// name of its own (full_page_database_own_name.go). A database of several
// keeps an icon of its own (database_own_icon.go).

const (
	databaseKeptIconKey     = "ownIcon"
	databaseKeptIconTypeKey = "ownIconType"
	databaseKeptIconForKey  = "ownIconFor"
)

type databaseOwnIconGivenKey struct{}

// withDatabaseOwnIcon marks a change to a database's one source that gives
// the database its icon, as an update of the database does, not of the
// source alone.
func withDatabaseOwnIcon(ctx context.Context) context.Context {
	return context.WithValue(ctx, databaseOwnIconGivenKey{}, true)
}

func databaseOwnIconGiven(ctx context.Context) bool {
	given := false
	if ctx != nil {
		given, _ = ctx.Value(databaseOwnIconGivenKey{}).(bool)
	}
	return given
}

// iconPair is an icon and its kind, "none" for no icon.
func iconPair(icon, iconType *string) (string, string) {
	if icon == nil || iconType == nil || *iconType == "" || *iconType == graphDataSourceIconNone || *icon == "" {
		return "", graphDataSourceIconNone
	}
	return *icon, *iconType
}

// pageIconPair is iconPair for a page's icon.
func pageIconPair(page *core.Record) (string, string) {
	icon, iconType := page.GetString("icon"), page.GetString("icon_type")
	return iconPair(&icon, &iconType)
}

// iconMark is an icon and its kind as one word, as ownIconFor keeps it.
func iconMark(icon, iconType string) string { return iconType + ":" + icon }

// keptDatabaseOwnIcon is the icon a full-page database's block keeps of its
// own while its page shows the one it was kept beside (shown).
func keptDatabaseOwnIcon(content []byte, shown string) (string, string, bool) {
	kept := struct {
		Icon     *string `json:"ownIcon"`
		IconType *string `json:"ownIconType"`
		For      string  `json:"ownIconFor"`
	}{}
	if json.Unmarshal(content, &kept) != nil || kept.IconType == nil || kept.For != shown {
		return "", "", false
	}
	icon, iconType := iconPair(kept.Icon, kept.IconType)
	return icon, iconType, true
}

func forgetKeptDatabaseIcon(content map[string]any) {
	delete(content, databaseKeptIconKey)
	delete(content, databaseKeptIconTypeKey)
	delete(content, databaseKeptIconForKey)
}

// writeDatabaseBlockContent changes a database's block as a change leaves
// it, in the change's own write of it or in one added; none where that
// leaves it as it was.
func writeDatabaseBlockContent(
	operations []graphOperation, stored storedDatabasePageBlock, change func(map[string]any),
) ([]graphOperation, error) {
	written := -1
	for index, operation := range operations {
		if operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil && operation.Block.ID == stored.ID {
			written = index
		}
	}
	base := json.RawMessage(stored.Content)
	if written >= 0 {
		base = operations[written].Block.Content
	}
	content, before := map[string]any{}, map[string]any{}
	if json.Unmarshal(base, &content) != nil || json.Unmarshal(base, &before) != nil {
		return operations, nil
	}
	if change(content); reflect.DeepEqual(content, before) {
		return operations, nil
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return operations, err
	}
	operations = append([]graphOperation(nil), operations...)
	if written >= 0 {
		block := *operations[written].Block
		block.Content = encoded
		operations[written].Block = &block
		return operations, nil
	}
	block := graphBlock{ID: stored.ID, Type: graphBlockTypeInlineDatabase, Position: stored.Position, Content: encoded,
		BaseContent: json.RawMessage(stored.Content)}
	return append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &block}), nil
}

// carrySourceIconToPage gives a full-page database's page the icon a graph
// change writes on its one source, when it changes it — read before the
// change is applied, so a write naming the source as it stands carries
// nothing. Set alone, its block keeps the icon the database had beside it;
// one its page led with, or the database's own given (withDatabaseOwnIcon),
// leaves none kept.
func carrySourceIconToPage(
	ctx context.Context, app core.App, ownerID string, authorization pageAuthorization, actorID string, input graphMutationInput,
) (graphMutationInput, error) {
	page := authorization.Page
	if input.PageKind != nil || page == nil || !isFullPageDatabase(page) {
		return input, nil
	}
	source, err := fullPageDatabaseTitleSource(app, ownerID, page.Id)
	if err != nil || source == nil {
		return input, err
	}
	storedIcon, storedType := iconPair(source.Icon, source.IconType)
	given := databaseOwnIconGiven(ctx)
	for _, operation := range input.Operations {
		written := operation.DataSource
		if operation.Kind != graphOperationKindUpsertDataSource || written == nil || written.ID != source.ID || written.IconType == nil {
			continue
		}
		icon, iconType := iconPair(written.Icon, written.IconType)
		if !given && icon == storedIcon && iconType == storedType {
			return input, nil
		}
		shown, now := iconMark(pageIconPair(page)), iconMark(icon, iconType)
		stored, found, err := loadDatabasePageBlock(app, ownerID, page.Id)
		if err != nil {
			return input, err
		}
		if found {
			own, ownType := pageIconPair(page)
			if keptIcon, keptType, kept := keptDatabaseOwnIcon([]byte(stored.Content), shown); kept {
				own, ownType = keptIcon, keptType
			}
			alone := !given && shown != now && iconMark(own, ownType) != now
			if input.Operations, err = writeDatabaseBlockContent(input.Operations, stored, func(content map[string]any) {
				forgetKeptDatabaseIcon(content)
				if alone {
					content[databaseKeptIconKey], content[databaseKeptIconTypeKey], content[databaseKeptIconForKey] = own, ownType, now
				}
			}); err != nil {
				return input, err
			}
		}
		if shown == now {
			return input, nil
		}
		return carryPageIcon(app, authorization, actorID, input, icon, iconType)
	}
	return input, nil
}

// restoreFullPageDatabaseOwnIcon gives a full-page database's page the icon
// its block kept of its own as a change makes it one of several sources; a
// kept icon gone unused is dropped.
func restoreFullPageDatabaseOwnIcon(
	app core.App, ownerID string, authorization pageAuthorization, actorID string, input graphMutationInput,
) (graphMutationInput, error) {
	page := authorization.Page
	if input.PageKind != nil || page == nil || !isFullPageDatabase(page) {
		return input, nil
	}
	stored, found, err := loadDatabasePageBlock(app, ownerID, page.Id)
	if err != nil || !found || !strings.Contains(stored.Content, `"`+databaseKeptIconForKey+`"`) {
		return input, err
	}
	for _, operation := range input.Operations {
		block := operation.Block
		if operation.Kind != graphOperationKindUpsertBlock || block == nil || block.ID != stored.ID ||
			block.Type != graphBlockTypeInlineDatabase || !pageNamedDatabase(block.Content) {
			continue
		}
		icon, iconType, kept := keptDatabaseOwnIcon([]byte(stored.Content), iconMark(pageIconPair(page)))
		if input.Operations, err = writeDatabaseBlockContent(input.Operations, stored, forgetKeptDatabaseIcon); err != nil || !kept {
			return input, err
		}
		return carryPageIcon(app, authorization, actorID, input, icon, iconType)
	}
	return input, nil
}

// carryPageIcon gives a database's page an icon as a change to it is
// applied, with the page's file references when it was or is an uploaded
// picture (giveChildPageCalloutIcon). The page's cursor the change was based
// on moves with it: the page changed with the change, not before it.
func carryPageIcon(
	app core.App, authorization pageAuthorization, actorID string, input graphMutationInput, icon, iconType string,
) (graphMutationInput, error) {
	page := authorization.Page
	based := input.BasePageCursor != nil && *input.BasePageCursor == page.GetInt("cursor")
	pictured := page.GetString("icon_type") == "image" || iconType == "image"
	committed, err := commitPageMetadataRecord(app, authorization, actorID, func(page *core.Record, _ int64) {
		page.Set("icon", icon)
		page.Set("icon_type", iconType)
	})
	if err == nil && pictured {
		err = replaceProductFilePageReferences(app, authorization.Workspace.WorkspaceID, page.Id, icon, iconType, committed.Page.GetString("cover"))
	}
	if err != nil {
		return input, err
	}
	if based {
		cursor := page.GetInt("cursor")
		input.BasePageCursor = &cursor
	}
	return input, nil
}

// databaseOwnIconOf is the icon the public API reads for a database of its
// own sources (count): one of several's own (databaseOfSeveralIconOf), a
// full-page one of one source's kept apart from it, else its source's.
func databaseOwnIconOf(
	app core.App, container notionDatabaseContainer, page *core.Record, count int, sourceIcon, sourceIconType string,
) (string, string) {
	if count > 1 {
		return databaseOfSeveralIconOf(app, container, page, sourceIcon, sourceIconType)
	}
	if !container.fullPage() || page == nil {
		return sourceIcon, sourceIconType
	}
	if icon, iconType, kept := keptDatabaseOwnIcon(databaseBlockContentOf(app, container), iconMark(pageIconPair(page))); kept {
		return icon, iconType
	}
	return sourceIcon, sourceIconType
}

// sourceIconFollowingPage is the write that gives a full-page database's one
// source its page's icon, nil when they show the same.
func sourceIconFollowingPage(page *core.Record, source *graphDataSource) *graphDataSource {
	if source == nil {
		return nil
	}
	icon, iconType := pageIconPair(page)
	storedIcon, storedType := iconPair(source.Icon, source.IconType)
	if icon == storedIcon && iconType == storedType {
		return nil
	}
	// One a data source can't have (linkedDatabaseIcon) stays the page's alone.
	if iconType != graphDataSourceIconNone {
		if _, _, ok := linkedDatabaseIcon(icon, iconType); !ok {
			return nil
		}
	}
	changed := *source
	changed.Icon, changed.IconType = &icon, &iconType
	return &changed
}

// nameFullPageDatabaseAfter names a full-page database's one source after
// its page's title and icon as a page details change outside the app's own
// writer gave them (nameFullPageDatabase).
func nameFullPageDatabaseAfter(ctx context.Context, app core.App, principalID, pageID string, titled, iconGiven bool) error {
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil || !titled && !iconGiven {
		return nil
	}
	return persistence.nameFullPageDatabase(ctx, principalID, pageID, titled, iconGiven)
}
