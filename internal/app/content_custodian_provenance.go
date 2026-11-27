package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const contentCustodianWindowLimit = 64

type contentCustodianItem struct {
	Kind         string `db:"kind"`
	ID           string `db:"id"`
	PageID       string `db:"page_id"`
	CreatedBy    string `db:"created_by"`
	LastEditedBy string `db:"last_edited_by"`
	Payload      string `db:"payload"`
}

func setNewPageCustodianProvenance(page *core.Record, actorID string) {
	page.Set("created_by", actorID)
	page.Set("last_edited_by", actorID)
}

func setPageLastEditor(page *core.Record, actorID string) {
	page.Set("last_edited_by", actorID)
}

// pageEditedNow is the time of an edit, as the page's updated time keeps it.
const pageEditedNow = `strftime('%Y-%m-%d %H:%M:%fZ', 'now')`

// setPageLastEditorByID notes who changed a page's text last, and when, as
// a change to its title is noted.
func setPageLastEditorByID(
	ctx context.Context,
	app core.App,
	pageID string,
	actorID string,
) error {
	return setPageLastEditorAt(ctx, app, pageID, actorID, time.Time{})
}

// setPageLastEditorAt notes who changed a page's text last and when the
// change came in, now where that isn't known. A later time the page already
// keeps stays: a change stored after it was made moves nothing back.
func setPageLastEditorAt(
	ctx context.Context,
	app core.App,
	pageID string,
	actorID string,
	editedAt time.Time,
) error {
	if strings.TrimSpace(pageID) == "" || strings.TrimSpace(actorID) == "" {
		return errors.New(errorCodeYgoPersistence)
	}
	if editedAt.IsZero() {
		editedAt = time.Now()
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET [[last_edited_by]] = {:actorId}, [[updated]] = MAX([[updated]], {:editedAt})
		WHERE [[id]] = {:pageId}
	`).WithContext(ctx).Bind(dbx.Params{
		"pageId": pageID, "actorId": actorID, "editedAt": editedAt.UTC().Format(types.DefaultDateLayout),
	}).Execute()
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errors.New(errorCodeYgoPersistence)
	}
	return nil
}

// laterYgoEditTime is the later of two update times, a zero one unknown.
func laterYgoEditTime(known, next time.Time) time.Time {
	if next.After(known) {
		return next
	}
	return known
}

// notePageContentEdited notes an edit to a page (noteGraphPageEdited): when, and by
// whom where someone made it. A body without a page record of its own, as a
// template's, notes nothing; nor does a page an import is still bringing in,
// whose body the import writes and which keeps the edit time it came with. A
// database page's own last edited time moves with it (noteRowPageBodyEdited).
func notePageContentEdited(ctx context.Context, app core.App, pageID, actorID string) error {
	if err := notePageRecordEdited(ctx, app, pageID, actorID); err != nil {
		return err
	}
	return noteRowPageBodyEdited(ctx, app, pageID)
}

// notePageRecordEdited notes an edit on a page's record alone.
func notePageRecordEdited(ctx context.Context, app core.App, pageID, actorID string) error {
	set, params := "[[updated]] = "+pageEditedNow, dbx.Params{"pageId": pageID}
	if strings.TrimSpace(actorID) != "" {
		set += ", [[last_edited_by]] = {:actorId}"
		params["actorId"] = actorID
	}
	_, err := app.DB().NewQuery("UPDATE {{leafen_pages}} SET " + set +
		" WHERE [[id]] = {:pageId} AND [[notion_import_staging]] = FALSE").
		WithContext(ctx).Bind(params).Execute()
	return err
}
