package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A migration's scan keeps what it finds as items beside the migration, a
// row each — its pages, databases, people and issues, the Notion ids it has
// seen and the queue of what is left to read — so a step reads and writes
// only what it touches and a list is read a window at a time, however large
// the Notion side is. The migration row keeps the counts.

const (
	notionMigrationItemPage     = "page"
	notionMigrationItemDatabase = "database"
	notionMigrationItemPerson   = "person"
	notionMigrationItemIssue    = "issue"
	notionMigrationItemSeen     = "seen"
	notionMigrationItemQueue    = "queue"
)

// notionMigrationScanStore reads and writes one migration's scan items.
type notionMigrationScanStore struct {
	app         core.App
	migrationID string
	workspaceID string
}

func (store notionMigrationScanStore) insert(kind, key string, ordinal int64, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = store.app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_migration_scan_items}} ([[migration_id]],[[workspace_id]],[[kind]],[[item_key]],[[ordinal]],[[value]])
		VALUES ({:migrationId},{:workspaceId},{:kind},{:key},{:ordinal},{:value})
		ON CONFLICT ([[migration_id]],[[kind]],[[item_key]]) DO NOTHING
	`).Bind(dbx.Params{"migrationId": store.migrationID, "workspaceId": store.workspaceID, "kind": kind, "key": key,
		"ordinal": ordinal, "value": string(encoded)}).Execute()
	return err
}

// nextOrdinal is the place after the last item of a kind.
func (store notionMigrationScanStore) nextOrdinal(kind string) (int64, error) {
	var last sql.NullInt64
	err := store.app.DB().NewQuery(`SELECT MAX([[ordinal]]) FROM {{leafen_notion_migration_scan_items}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]={:kind}`).
		Bind(dbx.Params{"migrationId": store.migrationID, "kind": kind}).Row(&last)
	return last.Int64 + 1, err
}

func (store notionMigrationScanStore) append(kind, key string, value any) error {
	ordinal, err := store.nextOrdinal(kind)
	if err != nil {
		return err
	}
	return store.insert(kind, key, ordinal, value)
}

// addPage and addDatabase keep what the scan read, a root's title in the
// migration's roots too (notion_migration_root_titles.go).
func (store notionMigrationScanStore) addPage(page notionMigrationPage) error {
	if err := store.titleRoot(page.ID, page.ParentID, page.Title); err != nil {
		return err
	}
	return store.append(notionMigrationItemPage, notionMigrationSourceKey(page.ID), page)
}

func (store notionMigrationScanStore) addDatabase(database notionMigrationDatabase) error {
	if err := store.titleRoot(database.ID, database.ParentID, database.Title); err != nil {
		return err
	}
	return store.append(notionMigrationItemDatabase, notionMigrationSourceKey(database.ID), database)
}

func (store notionMigrationScanStore) addPerson(person notionMigrationPerson) error {
	return store.append(notionMigrationItemPerson, person.ID, person)
}

// addIssue keeps an issue once.
func (store notionMigrationScanStore) addIssue(issue notionMigrationIssue) error {
	key := strings.Join([]string{issue.Code, notionMigrationSourceKey(issue.SourceID), issue.PropertyID, issue.Detail}, "\x00")
	return store.append(notionMigrationItemIssue, key, issue)
}

func (store notionMigrationScanStore) seen(id string) (bool, error) {
	_, found, err := store.find(notionMigrationItemSeen, notionMigrationSourceKey(id))
	return found, err
}

func (store notionMigrationScanStore) markSeen(id string) error {
	return store.insert(notionMigrationItemSeen, notionMigrationSourceKey(id), 0, map[string]any{})
}

func (store notionMigrationScanStore) find(kind, key string) (string, bool, error) {
	var value string
	err := store.app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_notion_migration_scan_items}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]={:kind} AND [[item_key]]={:key}`).
		Bind(dbx.Params{"migrationId": store.migrationID, "kind": kind, "key": key}).Row(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

// database is a scanned database by its Notion id.
func (store notionMigrationScanStore) database(id string) (notionMigrationDatabase, bool, error) {
	database := notionMigrationDatabase{}
	value, found, err := store.find(notionMigrationItemDatabase, notionMigrationSourceKey(id))
	if err != nil || !found {
		return database, found, err
	}
	return database, true, json.Unmarshal([]byte(value), &database)
}

// page is a scanned page by its Notion id.
func (store notionMigrationScanStore) page(id string) (notionMigrationPage, bool, error) {
	page := notionMigrationPage{}
	value, found, err := store.find(notionMigrationItemPage, notionMigrationSourceKey(id))
	if err != nil || !found {
		return page, found, err
	}
	return page, true, json.Unmarshal([]byte(value), &page)
}

func (store notionMigrationScanStore) putDatabase(database notionMigrationDatabase) error {
	encoded, err := json.Marshal(database)
	if err != nil {
		return err
	}
	_, err = store.app.DB().NewQuery(`UPDATE {{leafen_notion_migration_scan_items}} SET [[value]]={:value}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]='database' AND [[item_key]]={:key}`).
		Bind(dbx.Params{"value": string(encoded), "migrationId": store.migrationID, "key": notionMigrationSourceKey(database.ID)}).Execute()
	return err
}

// countBlocks adds blocks to a scanned page's count.
func (store notionMigrationScanStore) countBlocks(pageID string, blocks int) error {
	_, err := store.app.DB().NewQuery(`UPDATE {{leafen_notion_migration_scan_items}}
		SET [[value]]=json_set([[value]], '$.blocks', COALESCE(json_extract([[value]], '$.blocks'), 0) + {:blocks})
		WHERE [[migration_id]]={:migrationId} AND [[kind]]='page' AND [[item_key]]={:key}`).
		Bind(dbx.Params{"blocks": blocks, "migrationId": store.migrationID, "key": notionMigrationSourceKey(pageID)}).Execute()
	return err
}

// front is the queue's first item, its place, and whether there is one.
func (store notionMigrationScanStore) front() (notionMigrationScanItem, int64, bool, error) {
	row := struct {
		Ordinal int64  `db:"ordinal"`
		Value   string `db:"value"`
	}{}
	err := store.app.DB().NewQuery(`SELECT [[ordinal]],[[value]] FROM {{leafen_notion_migration_scan_items}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]='queue' ORDER BY [[ordinal]], [[item_key]] LIMIT 1`).
		Bind(dbx.Params{"migrationId": store.migrationID}).One(&row)
	item := notionMigrationScanItem{}
	if errors.Is(err, sql.ErrNoRows) {
		return item, 0, false, nil
	}
	if err != nil {
		return item, 0, false, err
	}
	return item, row.Ordinal, true, json.Unmarshal([]byte(row.Value), &item)
}

func (store notionMigrationScanStore) dropQueued(ordinal int64) error {
	_, err := store.app.DB().NewQuery(`DELETE FROM {{leafen_notion_migration_scan_items}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]='queue' AND [[item_key]]={:key}`).
		Bind(dbx.Params{"migrationId": store.migrationID, "key": strconv.FormatInt(ordinal, 10)}).Execute()
	return err
}

// pushFront puts items at the queue's front, in their order.
func (store notionMigrationScanStore) pushFront(items ...notionMigrationScanItem) error {
	if len(items) == 0 {
		return nil
	}
	var first sql.NullInt64
	if err := store.app.DB().NewQuery(`SELECT MIN([[ordinal]]) FROM {{leafen_notion_migration_scan_items}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]='queue'`).
		Bind(dbx.Params{"migrationId": store.migrationID}).Row(&first); err != nil {
		return err
	}
	start := int64(0) - int64(len(items))
	if first.Valid {
		start = first.Int64 - int64(len(items))
	}
	for index, item := range items {
		ordinal := start + int64(index)
		if err := store.insert(notionMigrationItemQueue, strconv.FormatInt(ordinal, 10), ordinal, item); err != nil {
			return err
		}
	}
	return nil
}

// window reads up to limit items of a kind after the place given (0 from
// the start); next is the place to go on from, 0 at the end.
func (store notionMigrationScanStore) window(kind string, after int64, limit int) ([]string, int64, error) {
	rows := []struct {
		Ordinal int64  `db:"ordinal"`
		Value   string `db:"value"`
	}{}
	if err := store.app.DB().NewQuery(`SELECT [[ordinal]],[[value]] FROM {{leafen_notion_migration_scan_items}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]={:kind} AND [[ordinal]] > {:after}
		ORDER BY [[ordinal]], [[item_key]] LIMIT {:limit}`).
		Bind(dbx.Params{"migrationId": store.migrationID, "kind": kind, "after": after, "limit": limit + 1}).All(&rows); err != nil {
		return nil, 0, err
	}
	next := int64(0)
	if len(rows) > limit {
		rows, next = rows[:limit], rows[limit-1].Ordinal
	}
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, row.Value)
	}
	return values, next, nil
}

// walk reads every item of a kind, a window at a time, into visit.
func (store notionMigrationScanStore) walk(kind string, visit func(string) error) error {
	after := int64(0)
	for {
		values, next, err := store.window(kind, after, notionMigrationScanWindow)
		if err != nil {
			return err
		}
		for _, value := range values {
			if err := visit(value); err != nil {
				return err
			}
		}
		if next == 0 {
			return nil
		}
		after = next
	}
}

// summary is the whole scan, for the plan check, the as-is draft and the
// report, which weigh every page and database together.
func (store notionMigrationScanStore) summary(counts notionMigrationCounts) (notionMigrationSummary, error) {
	summary := emptyNotionMigrationSummary()
	summary.Counts = counts
	for kind, add := range map[string]func(string) error{
		notionMigrationItemPage: func(value string) error {
			page := notionMigrationPage{}
			err := json.Unmarshal([]byte(value), &page)
			summary.Pages = append(summary.Pages, page)
			return err
		},
		notionMigrationItemDatabase: func(value string) error {
			database := notionMigrationDatabase{}
			err := json.Unmarshal([]byte(value), &database)
			summary.Databases = append(summary.Databases, database)
			return err
		},
		notionMigrationItemPerson: func(value string) error {
			person := notionMigrationPerson{}
			err := json.Unmarshal([]byte(value), &person)
			summary.People = append(summary.People, person)
			return err
		},
		notionMigrationItemIssue: func(value string) error {
			issue := notionMigrationIssue{}
			err := json.Unmarshal([]byte(value), &issue)
			summary.Issues = append(summary.Issues, issue)
			return err
		},
	} {
		if err := store.walk(kind, add); err != nil {
			return notionMigrationSummary{}, err
		}
	}
	return summary, nil
}

// readNotionMigrationScanItems reads a window of one kind of a migration's
// scan — pages, databases, issues or people — from the cursor on; an issue
// names the page or database it is about.
// size, where above 0, keeps a window to that many (?limit=); 0 is the
// kind's whole window.
func readNotionMigrationScanItems(app core.App, stored storedNotionMigration, kind, cursor string, size int) (notionMigrationScanItems, error) {
	output := notionMigrationScanItems{Pages: []notionMigrationPage{}, Databases: []notionMigrationDatabase{},
		Issues: []notionMigrationIssue{}, People: []notionMigrationPerson{}}
	after := int64(0)
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		parsed, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil || parsed <= 0 {
			return output, refuse(errNotionMigrationInvalid, "cursor is the nextCursor of the last window of this kind, or none to start")
		}
		after = parsed
	}
	itemKind, limit := map[string]string{
		"pages": notionMigrationItemPage, "databases": notionMigrationItemDatabase,
		"issues": notionMigrationItemIssue, "people": notionMigrationItemPerson,
	}[kind], notionMigrationScanWindow
	if itemKind == "" {
		return output, refuse(errNotionMigrationInvalid, "kind is pages, databases, issues or people, not %q", kind)
	}
	if itemKind == notionMigrationItemDatabase {
		limit = notionMigrationScanDatabaseWindow
	}
	if size > 0 && size < limit {
		limit = size
	}
	store := notionMigrationScanOf(app, stored)
	values, next, err := store.window(itemKind, after, limit)
	if err != nil {
		return output, err
	}
	if next > 0 {
		output.NextCursor = strconv.FormatInt(next, 10)
	}
	for _, value := range values {
		var err error
		switch itemKind {
		case notionMigrationItemPage:
			page := notionMigrationPage{}
			err = json.Unmarshal([]byte(value), &page)
			output.Pages = append(output.Pages, page)
		case notionMigrationItemDatabase:
			database := notionMigrationDatabase{}
			err = json.Unmarshal([]byte(value), &database)
			// With what the plan may do with each property.
			output.Databases = append(output.Databases, withNotionMigrationDatabaseChoices(database))
		case notionMigrationItemPerson:
			person := notionMigrationPerson{}
			err = json.Unmarshal([]byte(value), &person)
			output.People = append(output.People, person)
		case notionMigrationItemIssue:
			issue := notionMigrationIssue{}
			if err = json.Unmarshal([]byte(value), &issue); err == nil {
				issue.SourceTitle, err = store.title(issue.SourceID)
			}
			output.Issues = append(output.Issues, issue)
		}
		if err != nil {
			return notionMigrationScanItems{}, err
		}
	}
	return output, nil
}

// title is a scanned database's or page's title, by its Notion id.
func (store notionMigrationScanStore) title(id string) (string, error) {
	if id == "" {
		return "", nil
	}
	if database, found, err := store.database(id); err != nil || found {
		return database.Title, err
	}
	page, _, err := store.page(id)
	return page.Title, err
}
