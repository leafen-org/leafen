package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A mention of what is in the trash says, while the pointer rests on it, who
// sent it there and when, in place of its card, as Notion's does (seen
// 2026-11-24 in Chrome at 1440, two tabs: a page, a database and a data
// source, each sent there itself or under a page sent there — a page's and a
// database's "휴지통으로 이동한 사용자", a data source's "삭제한 사용자"). Its
// mark is the trash's own record of the move: a database's block gone from
// its page by who deleted it (leafen_deleted_databases, or the recoverable
// trash of its block), else the page in the trash holding it, itself or the
// nearest above it (loadPageTrashEffects) — by its row's mover for a database
// page, by its last editor for another page, as its trash banner names them
// (PageTrashBanner.tsx); a database page that went there with its database
// by its database's mark. A page's preview carries it (readPagePreview),
// which a mention of a page the reader's list lacks, a database page's, reads.

// mentionTrashMark is when what a mention names went to the trash (RFC 3339)
// and who sent it, either empty where the trash keeps none.
type mentionTrashMark struct {
	At string
	By string
}

// databaseMentionTrashMarkOf is the mark of a database in the trash, by its
// container's ID and the page holding it.
func databaseMentionTrashMarkOf(app core.App, containerID, pageID string) (mentionTrashMark, error) {
	deleted := struct {
		At int64  `db:"at"`
		By string `db:"by"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[deleted]].[[deleted_at]] AS [[at]], [[deleted]].[[deleted_by]] AS [[by]]
		FROM {{leafen_deleted_databases}} [[deleted]] WHERE [[deleted]].[[block_id]] = {:id}
		UNION ALL
		SELECT [[trash]].[[deleted_at]], COALESCE((
			SELECT [[tombstone]].[[deleted_by]] FROM {{leafen_block_trash_tombstones}} [[tombstone]]
			WHERE [[tombstone]].[[root_block_id]] = [[trash]].[[root_block_id]]
			ORDER BY [[tombstone]].[[deleted_at]] DESC LIMIT 1), '')
		FROM {{leafen_database_container_trash}} [[trash]] WHERE [[trash]].[[container_id]] = {:id}
		LIMIT 1
	`).Bind(dbx.Params{"id": containerID}).One(&deleted)
	if err == nil {
		return mentionTrashMark{At: time.UnixMilli(deleted.At).UTC().Format(time.RFC3339Nano), By: deleted.By}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return mentionTrashMark{}, err
	}
	return pageTrashMarkOf(app, pageID)
}

// pageTrashMarkOf is the mark of a page in the trash, itself or under a page
// there; none for a page out of it.
func pageTrashMarkOf(app core.App, pageID string) (mentionTrashMark, error) {
	_, mark, err := pageMentionTrashOf(app, pageID)
	return mark, err
}

// pageMentionTrashOf says whether a page is in the trash, itself or under a
// page there (loadPageTrashEffects, the rule a link to it reads), and its
// mark. A database item goes by its database where that is what went there
// — sent there alone from a page out of it, or a full-page database —
// rather than by itself, as Notion's mention of the row says who sent its
// database there and when (seen 2026-11-24 in Chrome at 1440, two tabs: a
// row sent there itself, one of a database under a page sent there, one of
// a full-page database sent there and one of an inline database sent there
// alone, each "휴지통으로 이동한 사용자:").
func pageMentionTrashOf(app core.App, pageID string) (bool, mentionTrashMark, error) {
	if pageID == "" {
		return false, mentionTrashMark{}, nil
	}
	effects, err := loadPageTrashEffects(app, []string{pageID})
	effect, found := effects[pageID]
	if err != nil || !found {
		return false, mentionTrashMark{}, err
	}
	root := struct {
		By         string `db:"by"`
		SourceID   string `db:"data_source_id"`
		RowTrashed bool   `db:"row_trashed"`
	}{}
	err = app.DB().NewQuery(`
		SELECT COALESCE([[row_trash]].[[trashed_by]], [[page]].[[last_edited_by]]) AS [[by]], [[page]].[[data_source_id]],
			[[row_trash]].[[row_id]] IS NOT NULL AS [[row_trashed]]
		FROM {{leafen_pages}} [[page]]
		LEFT JOIN {{leafen_database_row_trash}} [[row_trash]] ON [[row_trash]].[[row_id]] = [[page]].[[id]]
		WHERE [[page]].[[id]] = {:id}
	`).Bind(dbx.Params{"id": effect.RootID}).One(&root)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return true, mentionTrashMark{}, err
	}
	// An item in the trash by its database's going there, not its own.
	if root.SourceID != "" && !root.RowTrashed {
		_, containerID, holderID, held, err := mentionedDataSource(app, notionMarkdownDataSourceAddress+root.SourceID)
		if err != nil {
			return true, mentionTrashMark{}, err
		}
		if held && holderID != effect.RootID {
			mark, err := databaseMentionTrashMarkOf(app, containerID, holderID)
			return true, mark, err
		}
	}
	return true, mentionTrashMark{At: effect.TrashedAt, By: root.By}, nil
}
