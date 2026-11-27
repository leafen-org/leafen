package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A database page's icon, cover, style, archive state and who made and last
// edited it are its page record's, written here.

// rowPageRecordWhere matches a database page's record, in the given
// workspace and held by the database on the given page when they are named.
const rowPageRecordWhere = `[[id]]={:rowId} AND [[data_source_id]]!=''
	AND ({:workspaceId}='' OR [[workspace]]={:workspaceId})
	AND ({:pageId}='' OR EXISTS (
		SELECT 1 FROM {{leafen_database_row_pages}} AS [[row_page]]
		WHERE [[row_page]].[[row_id]]={:rowId} AND [[row_page]].[[container_page_id]]={:pageId}))`

func updateRowPageRecord(app core.App, set, where string, params dbx.Params) (bool, error) {
	result, err := app.DB().NewQuery(`UPDATE {{leafen_pages}} SET ` + set + ` WHERE ` + rowPageRecordWhere + where).
		Bind(params).Execute()
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

// setRowPageRecordEditor records who last edited a database page and
// reports whether the page was found.
func setRowPageRecordEditor(app core.App, rowID, workspaceID, actorID string) (bool, error) {
	return updateRowPageRecord(app, `[[last_edited_by]]={:actorId}`, "", dbx.Params{
		"rowId": rowID, "workspaceId": workspaceID, "pageId": "", "actorId": actorID,
	})
}

// setRowPageRecordAuthors records who made a database page, once, and who
// last edited it.
func setRowPageRecordAuthors(app core.App, rowID, workspaceID, actorID string) error {
	_, err := updateRowPageRecord(app, `
		[[created_by]]=CASE WHEN [[created_by]]='' THEN {:actorId} ELSE [[created_by]] END,
		[[last_edited_by]]={:actorId}`, "", dbx.Params{
		"rowId": rowID, "workspaceId": workspaceID, "pageId": "", "actorId": actorID,
	})
	return err
}

// setRowPageRecordIcon sets a database page's icon, and who last edited it
// when an editor is named; it reports whether the page was found.
func setRowPageRecordIcon(app core.App, rowID, workspaceID, containerPageID, actorID, icon, iconType string) (bool, error) {
	return updateRowPageRecord(app, `[[icon]]={:icon}, [[icon_type]]={:iconType},
		[[last_edited_by]]=CASE WHEN {:actorId}='' THEN [[last_edited_by]] ELSE {:actorId} END`, "", dbx.Params{
		"rowId": rowID, "workspaceId": workspaceID, "pageId": containerPageID,
		"actorId": actorID, "icon": icon, "iconType": iconType,
	})
}

// setRowPageRecordArchive moves a database page into or out of its archive
// and reports whether it changed; an archived page keeps its first archive.
func setRowPageRecordArchive(
	app core.App, rowID, workspaceID, containerPageID, actorID, archivedAt, archivedBy string, archived bool,
) (bool, error) {
	return updateRowPageRecord(app, `[[archived_at]]={:archivedAt}, [[archived_by]]={:archivedBy},
		[[last_edited_by]]={:actorId}`, ` AND (trim([[archived_at]])='') = {:archived}`, dbx.Params{
		"rowId": rowID, "workspaceId": workspaceID, "pageId": containerPageID, "actorId": actorID,
		"archivedAt": archivedAt, "archivedBy": archivedBy, "archived": archived,
	})
}

// setRowPageRecordCover sets a database page's cover and where it shows, as
// applyPageCover does: the place given, else the middle when the page showed
// no cover, else where it was; and who last edited it when an editor is
// named. It reports whether the page was found.
func setRowPageRecordCover(app core.App, rowID, workspaceID, actorID, cover string, coverPosition *int) (bool, error) {
	return updateRowPageRecord(app, `[[cover]]={:cover}, [[cover_position]]=COALESCE({:coverPosition},
		CASE WHEN [[cover]]='' AND {:cover}!='' THEN {:middle} ELSE [[cover_position]] END),
		[[last_edited_by]]=CASE WHEN {:actorId}='' THEN [[last_edited_by]] ELSE {:actorId} END`, "", dbx.Params{
		"rowId": rowID, "workspaceId": workspaceID, "pageId": "",
		"actorId": actorID, "cover": cover, "coverPosition": coverPosition, "middle": pageCoverMiddle,
	})
}

// setRowPageRecordStyle sets a database page's font, small text and full
// width where given, and who last edited it; it reports whether the page was
// found.
func setRowPageRecordStyle(app core.App, rowID, workspaceID, actorID string, font *string, smallText, fullWidth *bool) (bool, error) {
	return updateRowPageRecord(app, `[[font]]=COALESCE({:font}, [[font]]),
		[[small_text]]=COALESCE({:smallText}, [[small_text]]), [[full_width]]=COALESCE({:fullWidth}, [[full_width]]),
		[[last_edited_by]]=CASE WHEN {:actorId}='' THEN [[last_edited_by]] ELSE {:actorId} END`, "", dbx.Params{
		"rowId": rowID, "workspaceId": workspaceID, "pageId": "", "actorId": actorID,
		"font": font, "smallText": smallText, "fullWidth": fullWidth,
	})
}

// databaseRowPageFont is a database page's font as its page shows it: the
// default one until another is chosen.
func databaseRowPageFont(font string) string {
	if validPageFont(font) {
		return font
	}
	return pageFontDefault
}

// copyRowPageRecordPresentation gives a copied database page its source's
// icon, cover and style, and its archive when the source is archived; it
// reports whether both pages were found.
func copyRowPageRecordPresentation(app core.App, sourceRowID, targetRowID, workspaceID string, archive bool) (bool, error) {
	return copyRowPageRecordPresentationFrom(app, sourceRowID, workspaceID, targetRowID, workspaceID, archive)
}

// copyRowPageRecordPresentationFrom is copyRowPageRecordPresentation from a
// source in another workspace.
func copyRowPageRecordPresentationFrom(
	app core.App, sourceRowID, sourceWorkspaceID, targetRowID, workspaceID string, archive bool,
) (bool, error) {
	set := `[[icon]]=[[source]].[[icon]], [[icon_type]]=[[source]].[[icon_type]],
		[[cover]]=[[source]].[[cover]], [[cover_position]]=[[source]].[[cover_position]],
		[[font]]=[[source]].[[font]], [[small_text]]=[[source]].[[small_text]], [[full_width]]=[[source]].[[full_width]], [[table_of_contents_display]]=[[source]].[[table_of_contents_display]],
		[[inline_comments_display]]=[[source]].[[inline_comments_display]]`
	if archive {
		set = `[[archived_at]]=[[source]].[[archived_at]], [[archived_by]]=[[source]].[[archived_by]]`
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET ` + set + `
		FROM (SELECT [[icon]], [[icon_type]], [[cover]], [[cover_position]], [[font]], [[small_text]], [[full_width]], [[table_of_contents_display]], [[inline_comments_display]],
				[[archived_at]], [[archived_by]]
			FROM {{leafen_pages}}
			WHERE [[id]]={:sourceRowId} AND [[workspace]]={:sourceWorkspaceId} AND [[data_source_id]]!=''
				AND ({:archive}=0 OR trim([[archived_at]])!='')) AS [[source]]
		WHERE {{leafen_pages}}.[[id]]={:targetRowId} AND {{leafen_pages}}.[[workspace]]={:workspaceId}
			AND {{leafen_pages}}.[[data_source_id]]!=''
	`).Bind(dbx.Params{
		"sourceRowId": sourceRowID, "targetRowId": targetRowID, "workspaceId": workspaceID,
		"sourceWorkspaceId": sourceWorkspaceID, "archive": archive,
	}).Execute()
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

// announceDatabaseRowPageChange records a change on the page holding a
// database item's database once what its views draw of the item's own page
// changed — its icon or cover, its archive, its lock — so the views open
// there read it again, as after a change of its values or its sharing
// (2026-10-21: set from the sidebar or the API, they showed in an open table
// only once something else read it again). Imports, which set many at once,
// don't: their pages are read again when they finish. The change names the
// item, so the views read that item again, not their whole graph
// (2026-11-25: one icon set elsewhere had an open table of 5,000 rows read
// its 51 windows again, some 7 seconds on a NAS-class server).
func announceDatabaseRowPageChange(app core.App, rowID string) error {
	holder := struct {
		Owner string `db:"owner"`
		Page  string `db:"container_page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[owner]], [[container_page_id]] FROM {{leafen_database_row_pages}} WHERE [[row_id]]={:rowId} LIMIT 1
	`).Bind(dbx.Params{"rowId": rowID}).One(&holder); err != nil {
		return err
	}
	return recordServerGraphRowsChange(app, holder.Owner, holder.Page,
		"database-row-page:"+security.RandomString(20), []string{rowID})
}

// announceDatabaseRowPageLook tells of a database page's look written apart
// from a change of its database — its icon, cover or style a template gave
// it — as a look set from the page is: its revision moves on, and the views
// and peeks open on it read it again (announceDatabaseRowPageChange).
func announceDatabaseRowPageLook(app core.App, rowID string) error {
	if err := touchDatabaseRowPageAuthority(app, rowID, time.Now().UTC().UnixMilli()); err != nil {
		return err
	}
	return announceDatabaseRowPageChange(app, rowID)
}

// touchDatabaseRowPageAuthority moves a row page's revision on after its
// page record changed.
func touchDatabaseRowPageAuthority(app core.App, rowID string, now int64) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_row_pages}}
		SET [[authority_revision]]=[[authority_revision]]+1, [[updated]]={:now}
		WHERE [[row_id]]={:rowId}
	`).Bind(dbx.Params{"rowId": rowID, "now": now}).Execute()
	return err
}

func setRowPageRecordCustomization(app core.App, rowID, workspaceID, actorID string, outline, inline *string) (bool, error) {
	if outline == nil && inline == nil {
		return false, nil
	}
	return updateRowPageRecord(app, `[[table_of_contents_display]]=COALESCE({:outline}, [[table_of_contents_display]]),
 [[inline_comments_display]]=COALESCE({:inline}, [[inline_comments_display]]),
 [[last_edited_by]]=CASE WHEN {:actorId}='' THEN [[last_edited_by]] ELSE {:actorId} END`, "", dbx.Params{
		"rowId": rowID, "workspaceId": workspaceID, "pageId": "", "actorId": actorID, "outline": outline, "inline": inline,
	})
}
