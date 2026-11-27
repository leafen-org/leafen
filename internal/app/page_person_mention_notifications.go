package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A person @mentioned in a page's text is notified by the server, as in
// Notion, however the text was written. Each projection of a page's text
// compares the people its blocks mention with those it knew, admits one
// mention notification event per block for the newly mentioned people, and
// forgets mentions that were removed. A block notifies a person of its
// mention once on a page, so undoing a removal does not notify again; moving
// it to another page or turning it into a page does, as Notion's do (seen
// 2026-11-05). Delivery checks again that the mention is still there and that
// the person can open the page.

type blockPersonMentions struct {
	Editor  string
	Users   []string
	Preview string
	// Connection names the integration that last wrote the block, when one
	// did (noticeConnection): its mention's notice names it, as Notion's
	// inbox does.
	Connection string
}

type storedPagePersonMention struct {
	BlockID string `db:"block_id"`
	UserID  string `db:"user_id"`
}

// blockTextPeople lists the people a block's text @mentions.
func blockTextPeople(text *crdt.YText) []string {
	if text == nil {
		return nil
	}
	people := map[string]struct{}{}
	for _, delta := range textDeltaAsItStands(text.ToDelta(), false) {
		if delta.Op != crdt.DeltaOpInsert || delta.Attributes["mention"] != "person" {
			continue
		}
		if userID, ok := delta.Attributes["userId"].(string); ok && validCommentIdentifier(userID) {
			people[userID] = struct{}{}
		}
	}
	users := make([]string, 0, len(people))
	for userID := range people {
		users = append(users, userID)
	}
	sort.Strings(users)
	return users
}

// expectPagePersonMentions tells that a page someone has just made has been
// read: the mentions its first text makes are new and notify, as Notion's do
// (seen 2026-11-05), a copy made in the app's among them. A page that comes
// with its text otherwise, as an import or a connection's copy does, is left to
// its first reading, which notifies nobody.
func expectPagePersonMentions(app core.App, pageID string, now int64) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_person_mention_baselines}} ([[page_id]], [[projected_at]])
		VALUES ({:pageId}, {:now}) ON CONFLICT DO NOTHING
	`).Bind(dbx.Params{"pageId": pageID, "now": now}).Execute()
	return err
}

// movePagePersonMentions carries a moved block's mentions to its new page.
// There they are new, so the people are told again; undoing the move tells
// nobody.
func movePagePersonMentions(app core.App, workspaceID, actorID, blockID, fromPageID, toPageID string, tell bool) error {
	people := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[user_id]] FROM {{leafen_page_person_mentions}}
		WHERE [[page_id]] = {:pageId} AND [[block_id]] = {:blockId} ORDER BY [[user_id]]
	`).Bind(dbx.Params{"pageId": fromPageID, "blockId": blockID}).Column(&people); err != nil || len(people) == 0 {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE OR REPLACE {{leafen_page_person_mentions}} SET [[page_id]] = {:toPageId}
		WHERE [[page_id]] = {:pageId} AND [[block_id]] = {:blockId}
	`).Bind(dbx.Params{"toPageId": toPageID, "pageId": fromPageID, "blockId": blockID}).Execute(); err != nil {
		return err
	}
	if !tell {
		return nil
	}
	var preview string
	if err := app.DB().NewQuery(`
		SELECT [[preview]] FROM {{leafen_notification_events}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[source_kind]] = 'block' AND [[source_id]] = {:blockId}
		ORDER BY [[created_at]] DESC LIMIT 1
	`).Bind(dbx.Params{"workspaceId": workspaceID, "blockId": blockID}).Row(&preview); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	page, err := app.FindRecordById(pagesCollection, toPageID)
	if err != nil {
		return err
	}
	target, err := pageNotificationTarget(app, toPageID)
	if err != nil {
		return err
	}
	if target == "/p/"+url.PathEscape(toPageID) {
		target += pageBlockAnchor(app, toPageID, blockID)
	}
	return tellPagePersonMention(app, toPageID, workspaceID, page.GetString("title"), blockID, target, actorID, preview, people)
}

// tellChildPagePersonMentions tells the people a block turned into a page
// mentions of that page, as Notion's does: its title carries them. Their
// mention stays the block's, on the page holding it (pagePersonMentionCurrent).
func tellChildPagePersonMentions(app core.App, workspaceID, title, blockID, actorID string, text *crdt.YText) error {
	people := blockTextPeople(text)
	if len(people) == 0 {
		return nil
	}
	return tellPagePersonMention(app, blockID, workspaceID, title, blockID, "/p/"+url.PathEscape(blockID), actorID, title, people)
}

func tellPagePersonMention(
	app core.App, pageID, workspaceID, title, blockID, target, actorID, preview string, people []string,
) error {
	recipients := []string{}
	for _, userID := range people {
		if userID != actorID {
			recipients = append(recipients, userID)
		}
	}
	if len(recipients) == 0 || actorID == "" {
		return nil
	}
	// A move or a page made of a block is the writing transaction's, named
	// by the connection it writes through, if one.
	if err := insertPagePersonMentionEvent(app, pageID, workspaceID, title, blockID, target, blockPersonMentions{
		Editor: actorID, Users: recipients, Preview: preview, Connection: noticeConnection(pageHistoryConnectionOf(app)),
	}, recipients, time.Now().UTC()); err != nil {
		return err
	}
	notifyCommentNotificationMaterializationAfterCommit(app)
	return nil
}

func projectPagePersonMentions(
	ctx context.Context,
	app core.App,
	pageID, workspaceID, title string,
	mentions map[string]blockPersonMentions,
	now time.Time,
) error {
	var baselined int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_page_person_mention_baselines}} WHERE [[page_id]] = {:pageId}
	`).WithContext(ctx).Bind(dbx.Params{"pageId": pageID}).Row(&baselined); err != nil {
		return err
	}
	stored := []storedPagePersonMention{}
	if err := app.DB().NewQuery(`
		SELECT [[block_id]], [[user_id]] FROM {{leafen_page_person_mentions}} WHERE [[page_id]] = {:pageId}
	`).WithContext(ctx).Bind(dbx.Params{"pageId": pageID}).All(&stored); err != nil {
		return err
	}
	known := make(map[string]struct{}, len(stored))
	for _, mention := range stored {
		key := mention.BlockID + "\x00" + mention.UserID
		known[key] = struct{}{}
		if block, found := mentions[mention.BlockID]; found && sortedContains(block.Users, mention.UserID) {
			continue
		}
		if _, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_page_person_mentions}}
			WHERE [[page_id]] = {:pageId} AND [[block_id]] = {:blockId} AND [[user_id]] = {:userId}
		`).WithContext(ctx).Bind(dbx.Params{
			"pageId": pageID, "blockId": mention.BlockID, "userId": mention.UserID,
		}).Execute(); err != nil {
			return err
		}
	}
	blockIDs := make([]string, 0, len(mentions))
	for blockID := range mentions {
		blockIDs = append(blockIDs, blockID)
	}
	sort.Strings(blockIDs)
	admitted := false
	target, err := pageNotificationTarget(app, pageID)
	if err != nil {
		return err
	}
	for _, blockID := range blockIDs {
		block := mentions[blockID]
		fresh := []string{}
		for _, userID := range block.Users {
			if _, found := known[blockID+"\x00"+userID]; found {
				continue
			}
			// Someone who can't open the page yet isn't told of the mention,
			// and it isn't counted as told either: once the page is shared, the
			// block's mention notifies them (2026-10-27: a mention made before
			// sharing swallowed the one typed after it).
			if baselined != 0 && userID != block.Editor {
				if _, authErr := authorizeCommentNotificationTarget(app, userID, pageID); authErr != nil {
					if isPermissionDenied(authErr) {
						continue
					}
					return authErr
				}
			}
			if _, err := app.DB().NewQuery(`
				INSERT INTO {{leafen_page_person_mentions}} ([[page_id]], [[block_id]], [[user_id]], [[created_at]])
				VALUES ({:pageId}, {:blockId}, {:userId}, {:now})
			`).WithContext(ctx).Bind(dbx.Params{
				"pageId": pageID, "blockId": blockID, "userId": userID, "now": now.UnixMilli(),
			}).Execute(); err != nil {
				return err
			}
			if userID == block.Editor {
				continue
			}
			notified, err := pagePersonMentionNotified(ctx, app, workspaceID, blockID, userID)
			if err != nil {
				return err
			}
			if !notified {
				fresh = append(fresh, userID)
			}
		}
		if baselined == 0 || len(fresh) == 0 || block.Editor == "" {
			continue
		}
		// On the page itself, the mention's block.
		blockTarget := target
		if blockTarget == "/p/"+url.PathEscape(pageID) {
			blockTarget += pageBlockAnchor(app, pageID, blockID)
		}
		if err := insertPagePersonMentionEvent(app, pageID, workspaceID, title, blockID, blockTarget, block, fresh, now); err != nil {
			return err
		}
		admitted = true
	}
	if baselined == 0 {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_page_person_mention_baselines}} ([[page_id]], [[projected_at]])
			VALUES ({:pageId}, {:now})
		`).WithContext(ctx).Bind(dbx.Params{"pageId": pageID, "now": now.UnixMilli()}).Execute(); err != nil {
			return err
		}
	}
	if admitted {
		notifyCommentNotificationMaterializationAfterCommit(app)
	}
	return nil
}

func insertPagePersonMentionEvent(
	app core.App,
	pageID, workspaceID, title, blockID, target string,
	block blockPersonMentions,
	recipients []string,
	now time.Time,
) error {
	mentionJSON, err := notificationRecipientJSON(recipients)
	if err != nil {
		return err
	}
	if title == "" {
		title = "Untitled"
	}
	revision := now.UnixMilli()
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"page-person-mention", workspaceID, blockID, strconv.FormatInt(revision, 10),
	}, "\x00")))
	created := now.UTC().Format(time.RFC3339Nano)
	_, err = app.DB().Insert(notificationEventsTable, dbx.Params{
		"id": hex.EncodeToString(digest[:16]), "workspace_id": workspaceID, "source_kind": "block",
		"source_id": blockID, "source_revision": revision, "actor_id": block.Editor, "actor_connection": block.Connection,
		"page_id": pageID, "block_id": blockID, "parent_comment_id": "",
		"title": notificationEventTitle(title), "preview": block.Preview,
		"target":                     target,
		"mention_recipient_ids_json": mentionJSON, "fixed_recipient_ids_json": "[]",
		"include_page_audience": false, "audience_at": now.UnixMilli(), "status": "pending",
		"created_at": created, "updated_at": created,
	}).Execute()
	return err
}

// pagePersonMentionNotified reports whether a block already notified the
// person of a mention, as a pending event or a notification. An event whose
// delivery to them was skipped (they couldn't open the page, or the mention
// was gone) told them nothing.
func pagePersonMentionNotified(ctx context.Context, app core.App, workspaceID, blockID, userID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT (SELECT COUNT(*) FROM {{leafen_notification_events}} AS [[event]]
			WHERE [[event]].[[workspace_id]] = {:workspaceId} AND [[event]].[[source_kind]] = 'block'
				AND [[event]].[[source_id]] = {:blockId}
				AND EXISTS (SELECT 1 FROM json_each([[event]].[[mention_recipient_ids_json]]) AS [[recipient]]
					WHERE [[recipient]].[[value]] = {:userId})
				AND NOT EXISTS (SELECT 1 FROM {{leafen_notification_deliveries}} AS [[delivery]]
					WHERE [[delivery]].[[event_id]] = [[event]].[[id]] AND [[delivery]].[[user_id]] = {:userId}
						AND [[delivery]].[[status]] = 'skipped')) +
			(SELECT COUNT(*) FROM {{leafen_notifications}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[user_id]] = {:userId}
				AND [[block_id]] = {:blockId} AND [[kind]] = 'mention' AND [[comment_id]] = '')
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID, "blockId": blockID, "userId": userID,
	}).Row(&count)
	return count > 0, err
}

// pagePersonMentionCurrent reports whether a block still mentions the
// person in the page's text; a database page in the trash mentions no one.
// A mention noticed before database pages kept their own mentions names the
// page holding the database, so a database page on it answers for it too; a
// block turned into a page keeps its mention on the page holding it.
func pagePersonMentionCurrent(ctx context.Context, app core.App, pageID, blockID, userID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_page_person_mentions}} [[mention]]
		WHERE [[mention]].[[block_id]] = {:blockId} AND [[mention]].[[user_id]] = {:userId}
			AND [[mention]].[[page_id]] IN (
				SELECT {:pageId}
				UNION ALL
				SELECT [[block]].[[page_id]] FROM {{leafen_blocks}} [[block]]
				JOIN {{leafen_pages}} [[row]] ON [[row]].[[id]] = [[block]].[[page_id]] AND [[row]].[[data_source_id]] != ''
				JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[row]].[[data_source_id]]
					AND [[source]].[[page_id]] = {:pageId}
				WHERE [[block]].[[id]] = {:blockId}
				UNION ALL
				SELECT [[page]].[[parent_id]] FROM {{leafen_pages}} [[page]]
				WHERE [[page]].[[id]] = {:pageId} AND {:pageId} = {:blockId}
			)
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_row_trash}} [[trash]] WHERE [[trash]].[[row_id]] = [[mention]].[[page_id]])
	`).WithContext(ctx).Bind(dbx.Params{"pageId": pageID, "blockId": blockID, "userId": userID}).Row(&count)
	return count > 0, err
}

// pageNotificationTarget is where a notice about a page opens it: at its own
// address, a database page too (databaseRowPageTarget); "" for a page no
// longer there or a database page whose database is gone.
func pageNotificationTarget(app core.App, pageID string) (string, error) {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// A page's own address, which a notice's email links to as well as its
	// inbox row (2026-10-16: an access granted said so with no way to open it).
	if !isDatabaseRowPageRecord(page) {
		return "/p/" + url.PathEscape(pageID), nil
	}
	if _, err := databaseRowPageContainer(app, page); isPermissionDenied(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	return databaseRowPageTarget(pageID), nil
}

// databaseRowPageTarget is where a notice about a database item opens it:
// as a full page at its own address, as Notion's inbox opens an item it
// names (seen 2026-11-09), not over the page holding its database. Emails
// and pushes link there too.
func databaseRowPageTarget(rowID string) string {
	return "/p/" + url.PathEscape(rowID)
}

func sortedContains(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}
