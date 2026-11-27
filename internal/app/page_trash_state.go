package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// applyPageTrashState is the single writer for reversible page-trash state.
// It preserves the first timestamp while a page remains trashed, clears it on
// restore, and never derives deletion time from unrelated page recency.
func applyPageTrashState(page *core.Record, inTrash bool, transitionAt string) {
	if !inTrash {
		page.Set("in_trash", false)
		page.Set("trashed_at", "")
		return
	}
	if page.GetBool("in_trash") {
		if existing := strings.TrimSpace(page.GetString("trashed_at")); existing != "" {
			page.Set("in_trash", true)
			return
		}
	}
	transitionAt = strings.TrimSpace(transitionAt)
	if transitionAt == "" {
		transitionAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	page.Set("in_trash", inTrash)
	page.Set("trashed_at", transitionAt)
}

func pageTrashTimestamp(page *core.Record) *string {
	return optionalPageTrashTimestamp(page.GetString("trashed_at"))
}

func optionalPageTrashTimestamp(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

// pageInTrashPath reports whether a page is in the trash or sits under a page
// that is: a sub-page goes to the trash with the page that holds it.
func pageInTrashPath(app core.App, pageID string) (bool, error) {
	_, trashed, err := loadPageTrashEffect(app, pageID)
	return trashed, err
}

const errorCodePageInTrash = "LEAFEN_PAGE_IN_TRASH"

// pageTrashEffect names the page whose trash holds a page: the page itself or
// its nearest ancestor in the trash. A sub-page goes to the trash with the
// page that holds it and comes back when that page is restored, while a
// sub-page trashed on its own stays in the trash.
type pageTrashEffect struct {
	PageID    string `db:"page_id"`
	RootID    string `db:"root_id"`
	TrashedAt string `db:"trashed_at"`
}

// pageTrashWalkSQL walks from the pages the seed condition picks
// ([[walk_page]]) up through the pages above each (pageAncestryParentSQL) to
// the first one in the trash itself (pageInTrashSQL), as [[trash_walk]]
// (page_id, id, parent_id, in_trash, trashed_at): the one rule of what the
// trash holds — a page in it, and every page and database item under it —
// which loadPageTrashEffects reads and pageOutOfTrashPathSQL asks inside a
// list's own query.
func pageTrashWalkSQL(seed string) string {
	return `WITH RECURSIVE [[trash_walk]] ([[page_id]], [[id]], [[parent_id]], [[in_trash]], [[trashed_at]]) AS (
			SELECT [[walk_page]].[[id]], [[walk_page]].[[id]], ` + pageAncestryParentSQL("[[walk_page]]") + `,
				` + pageInTrashSQL("[[walk_page]]") + `, ` + pageTrashedAtSQL("[[walk_page]]") + `
			FROM {{leafen_pages}} [[walk_page]] WHERE ` + seed + `
			UNION
			SELECT [[trash_walk]].[[page_id]], [[walk_page]].[[id]], ` + pageAncestryParentSQL("[[walk_page]]") + `,
				` + pageInTrashSQL("[[walk_page]]") + `, ` + pageTrashedAtSQL("[[walk_page]]") + `
			FROM [[trash_walk]] JOIN {{leafen_pages}} [[walk_page]] ON [[walk_page]].[[id]] = [[trash_walk]].[[parent_id]]
			WHERE [[trash_walk]].[[parent_id]] != '' AND NOT [[trash_walk]].[[in_trash]]
		)`
}

// pageOutOfTrashPathSQL holds for the page the expression names while it is
// out of the trash with every page above it (pageTrashWalkSQL), for a list
// that counts in its own query what it lists. The expression is a bound
// value or a column of the outer query qualified by its own alias.
func pageOutOfTrashPathSQL(pageIDExpression string) string {
	return `NOT EXISTS (` + pageTrashWalkSQL(`[[walk_page]].[[id]] = `+pageIDExpression) + `
		SELECT 1 FROM [[trash_walk]] WHERE [[trash_walk]].[[in_trash]])`
}

// loadPageTrashEffects is the single reader of effective page trash state.
// Pages outside the trash are absent from the result.
func loadPageTrashEffects(app core.App, pageIDs []string) (map[string]pageTrashEffect, error) {
	effects := map[string]pageTrashEffect{}
	if len(pageIDs) == 0 {
		return effects, nil
	}
	params := dbx.Params{}
	placeholders := make([]string, 0, len(pageIDs))
	for index, pageID := range pageIDs {
		name := "page" + strconv.Itoa(index)
		params[name] = pageID
		placeholders = append(placeholders, "{:"+name+"}")
	}
	rows := []pageTrashEffect{}
	err := app.DB().NewQuery(pageTrashWalkSQL(`[[walk_page]].[[id]] IN (`+strings.Join(placeholders, ",")+`)`) + `
		SELECT [[page_id]], [[id]] AS [[root_id]], [[trashed_at]] FROM [[trash_walk]] WHERE [[in_trash]]
	`).Bind(params).All(&rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		effects[row.PageID] = row
	}
	return effects, nil
}

func loadPageTrashEffect(app core.App, pageID string) (pageTrashEffect, bool, error) {
	// Read once a read scope: a graph read asked it for editing and for
	// suggesting (2026-10-17); many may be read ahead together
	// (primePageTrashEffectsRemembered).
	found, err := remembered(app, pageTrashEffectMemoKey{pageID}, func() (pageTrashEffectMemoValue, error) {
		effects, err := loadPageTrashEffects(app, []string{pageID})
		if err != nil {
			return pageTrashEffectMemoValue{}, err
		}
		effect, trashed := effects[pageID]
		return pageTrashEffectMemoValue{effect: effect, trashed: trashed}, nil
	})
	return found.effect, found.trashed, err
}

// primePageTrashEffectsRemembered reads the trash state of pages together
// into a read scope's memo, where loadPageTrashEffect then finds each as it
// would have read it alone: a page's Markdown asked the trash of each page
// it names (2026-11-25: 2,100 reads for a page naming its 2,100 sub-pages).
// Outside a read scope it reads nothing.
func primePageTrashEffectsRemembered(app core.App, pageIDs []string) error {
	holder, ok := app.(accessMemoHolder)
	if !ok {
		return nil
	}
	memo := holder.accessMemo()
	wanted, listed := make([]string, 0, len(pageIDs)), map[string]bool{}
	for _, pageID := range pageIDs {
		if _, known := memo[pageTrashEffectMemoKey{pageID}]; !known && pageID != "" && !listed[pageID] {
			listed[pageID] = true
			wanted = append(wanted, pageID)
		}
	}
	for start := 0; start < len(wanted); start += 500 {
		part := wanted[start:min(start+500, len(wanted))]
		effects, err := loadPageTrashEffects(app, part)
		if err != nil {
			return err
		}
		for _, pageID := range part {
			effect, trashed := effects[pageID]
			memo[pageTrashEffectMemoKey{pageID}] = accessMemoEntry{value: pageTrashEffectMemoValue{effect: effect, trashed: trashed}}
		}
	}
	return nil
}

type pageTrashEffectMemoKey struct{ pageID string }

type pageTrashEffectMemoValue struct {
	effect  pageTrashEffect
	trashed bool
}

// pageInTrashError refuses a change to a page in the trash, as Notion keeps a
// trashed page read-only until it is restored.
func pageInTrashError() error {
	return &graphRequestError{
		status: http.StatusConflict, code: errorCodePageInTrash, message: pageInTrashMessage,
	}
}

const pageInTrashMessage = "A page in Trash can be edited after it's restored."

// placePageMetadataInputAgainstTrash keeps a page in the trash read-only and
// places a restored page where it can be seen. A page trashed on its own may
// only be restored; a restore whose parent is gone or still in the trash
// lands at the top of its section, as the page it lived under cannot show
// it. A sub-page of a trashed page leaves the trash only by moving out of it.
// Nothing moves into a page in the trash.
func placePageMetadataInputAgainstTrash(
	app core.App,
	page *core.Record,
	input pageMetadataInput,
) (pageMetadataInput, error) {
	effect, trashed, err := loadPageTrashEffect(app, page.Id)
	if err != nil {
		return input, err
	}
	parentID := page.GetString("parent_id")
	switch {
	case trashed && effect.RootID == page.Id && input.InTrash:
		return input, pageInTrashError()
	case trashed && effect.RootID == page.Id:
		if input.ParentID == parentID && parentID != "" {
			hidden, err := pageParentHidesRestore(app, page, parentID)
			if err != nil {
				return input, err
			}
			if hidden {
				input.ParentID = ""
			}
		}
	case trashed && !input.InTrash && input.ParentID == parentID:
		return input, pageInTrashError()
	}
	if input.ParentID != "" && input.ParentID != parentID {
		destinationTrashed, err := pageInTrashPath(app, input.ParentID)
		if err != nil {
			return input, err
		}
		if destinationTrashed {
			return input, pageInTrashError()
		}
	}
	return input, nil
}

func pageParentHidesRestore(app core.App, page *core.Record, parentID string) (bool, error) {
	parent, err := app.FindRecordById(pagesCollection, parentID)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if parent.GetString("workspace") != page.GetString("workspace") {
		return true, nil
	}
	return pageInTrashPath(app, parentID)
}
