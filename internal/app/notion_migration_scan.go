package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// The structure scan of a design-first Notion migration (B5, decision
// "설계 우선 Notion 이사" 2·4절): the page tree under the chosen roots with
// each page's block count, each database's properties (and the Leafen type
// each becomes), its row count up to a cap, a few sample rows and whether
// Notion gives its views, and what cannot move — the import's own loss
// reasons, found from the structure alone. It reads in steps of a bounded
// number of Notion requests and keeps where it is, so a person's screen or
// an agent calls it again until it is done; bodies are counted, never read
// out (decision 3절 원칙 5), and the Notion API version stays frozen (5절).
// It goes to the end of the tree, however large and deep: what it finds is
// kept as items (notion_migration_scan_items.go), each read Notion answer
// written with the queue in one transaction.

type notionMigrationScanItem struct {
	Kind     string `json:"kind"` // comments, page, blocks, database, source, rows, rowbody, rowinner, views, search, person
	ID       string `json:"id"`
	ParentID string `json:"parentId,omitempty"`
	// The page a block list belongs to, and how deep under it.
	PageID string `json:"pageId,omitempty"`
	Depth  int    `json:"depth,omitempty"`
	Cursor string `json:"cursor,omitempty"`
	// The data source a database's rows, or the properties of one after its
	// first, are read through, for a database of several; for the search,
	// the Notion version its cursor is of (notion_migration_scan_search.go).
	Source string `json:"source,omitempty"`
	// A pass of the search: when what it looks under was made
	// (notionMigrationScanPass).
	notionMigrationScanPass
}

// notionMigrationScanState was the scan's queue and seen ids when the scan
// was kept whole in its migration's row; only moving those scans reads it.
type notionMigrationScanState struct {
	Queue []notionMigrationScanItem `json:"queue"`
	Seen  map[string]bool           `json:"seen"`
}

func newNotionMigrationScanState(roots []notionImportRoot) notionMigrationScanState {
	state := notionMigrationScanState{Seen: map[string]bool{}, Queue: notionMigrationScanFirst(roots)}
	for _, root := range roots {
		kind := "page"
		if root.Kind == "database" {
			kind = "database"
		}
		state.Queue = append(state.Queue, notionMigrationScanItem{Kind: kind, ID: root.ID})
	}
	// Last, what the tree does not show: pages and databases in rows' bodies.
	state.Queue = append(state.Queue, notionMigrationScanItem{Kind: "search", notionMigrationScanPass: notionMigrationScanPass{Dated: true}})
	return state
}

// notionMigrationScanChange is what one Notion answer adds to the scan,
// written together with taking its item off the queue.
type notionMigrationScanChange struct {
	queue     []notionMigrationScanItem
	seen      []string
	pages     []notionMigrationPage
	blocks    map[string]int
	databases []notionMigrationDatabase
	updated   *notionMigrationDatabase
	issues    []notionMigrationIssue
	people    []notionMigrationPerson
	rows      int
	// A sampled row's body (notion_migration_scan_sample.go), and when what
	// was read was made (notion_migration_scan_search.go).
	sample notionMigrationRowSample
	made   notionMigrationScanMade
	// Requests beyond the item's own (notionMigrationScanHeadings).
	extra int
}

// scanNotionMigrationStep spends at most notionMigrationRequestsPerStep
// Notion requests and reports whether the scan is done. Each item read is
// written at once, so a step cut short keeps what it read. It times its
// requests, each from the end of the one before with its writing, as a
// run's reading goes one at a time (notionMigrationReadSeconds).
func scanNotionMigrationStep(
	ctx context.Context, client notionProviderClient, store notionMigrationScanStore, counts *notionMigrationCounts,
) (bool, error) {
	at := time.Now()
	for budget := notionMigrationRequestsPerStep; budget > 0; budget-- {
		item, ordinal, found, err := store.front()
		if err != nil || !found {
			return !found, err
		}
		change := notionMigrationScanChange{blocks: map[string]int{}}
		if err = scanNotionMigrationItem(ctx, client, store, &change, item); err != nil {
			// The item stays first, to be tried again on the next step.
			return false, err
		}
		counts.Requests, budget = counts.Requests+1+change.extra, budget-change.extra
		counts.ScanMs += time.Since(at).Milliseconds()
		at = time.Now()
		counts.Pages += len(change.pages)
		counts.Databases += len(change.databases)
		counts.Rows += change.rows
		change.sample.count(counts)
		for _, blocks := range change.blocks {
			counts.Blocks += blocks
		}
		if err := applyNotionMigrationScanChange(store, ordinal, change, counts); err != nil {
			return false, err
		}
	}
	_, _, found, err := store.front()
	return !found, err
}

func scanNotionMigrationPage(
	ctx context.Context, client notionProviderClient, store notionMigrationScanStore, change *notionMigrationScanChange,
	item notionMigrationScanItem,
) error {
	if seen, err := store.seen(item.ID); err != nil || seen {
		return err
	}
	page, err := client.page(ctx, item.ID)
	if err != nil {
		return err
	}
	change.seen = append(change.seen, item.ID)
	if err := notionMigrationAuthors(store, change, page); err != nil {
		return err
	}
	change.pages = append(change.pages, notionMigrationPage{ID: page.ID, Title: notionObjectTitle(page), ParentID: item.ParentID})
	change.made.note(page)
	change.queue = append(change.queue, notionMigrationScanItem{Kind: "blocks", ID: page.ID, PageID: page.ID})
	return nil
}

func scanNotionMigrationBlocks(
	ctx context.Context, client notionProviderClient, migrationID string, change *notionMigrationScanChange,
	item notionMigrationScanItem,
) error {
	listed, err := client.children(ctx, item.ID, item.Cursor)
	if err != nil {
		return err
	}
	blocks, err := notionProviderPageResults(listed)
	if err != nil {
		return err
	}
	change.made.note(blocks...)
	for _, block := range blocks {
		change.blocks[item.PageID]++
		notionMigrationBlockIssue(change, item.PageID, block)
		notionMigrationMentionedPeople(change, block)
		switch {
		case block.Type == "child_page":
			change.queue = append(change.queue, notionMigrationScanItem{Kind: "page", ID: block.ID, ParentID: item.PageID})
		case block.Type == "child_database":
			change.queue = append(change.queue, notionMigrationScanItem{Kind: "database", ID: block.ID, ParentID: item.PageID})
		case notionMigrationScanReads(block):
			change.queue = append(change.queue, notionMigrationScanItem{Kind: "blocks", ID: block.ID, PageID: item.PageID, Depth: item.Depth + 1})
		}
	}
	if err := notionMigrationScanHeadings(ctx, client, migrationID, change, item, blocks); err != nil {
		return err
	}
	if listed.HasMore && listed.NextCursor != nil {
		more := item
		more.Cursor = *listed.NextCursor
		change.queue = append(change.queue, more)
	}
	return nil
}

func scanNotionMigrationDatabase(
	ctx context.Context, client notionProviderClient, store notionMigrationScanStore, change *notionMigrationScanChange,
	item notionMigrationScanItem,
) error {
	if seen, err := store.seen(item.ID); err != nil || seen {
		return err
	}
	database, err := client.database(ctx, item.ID)
	// A database of several data sources is read through them
	// (notion_provider_data_sources.go), its rows through each.
	sources, first, others := []string{}, notionProviderOtherDataSource{}, []notionProviderOtherDataSource{}
	if notionProviderRefused(err) {
		if many, firstSource, otherSources, manyErr := client.databaseOfSources(ctx, item.ID, err); manyErr == nil {
			database, err, first, others, sources = many, nil, firstSource, otherSources, []string{firstSource.ID}
			for _, other := range others {
				sources = append(sources, other.ID)
			}
		}
	}
	if err != nil {
		if notionProviderRefused(err) {
			change.seen = append(change.seen, item.ID)
			// A linked database shows another's rows through views of its own,
			// which come over with it; it has nothing of its own to plan.
			views, viewsErr := client.databaseViews(ctx, item.ID, "")
			if viewsErr != nil && !notionProviderRefused(viewsErr) {
				return viewsErr
			}
			if viewsErr != nil || len(views.IDs) == 0 {
				change.issues = append(change.issues, notionMigrationIssue{
					Code: notionImportIssueDatabaseUnavailable, SourceID: item.ID, ParentID: item.ParentID,
				})
			}
			return nil
		}
		return err
	}
	change.seen = append(change.seen, item.ID)
	entry := notionMigrationDatabase{
		ID: database.ID, Title: notionMigrationScanDatabaseTitle(store, database, first, others), ParentID: item.ParentID,
		Inline:     database.IsInline != nil && *database.IsInline,
		Properties: notionMigrationScanProperties(change, database, notionMigrationIssue{SourceID: database.ID}),
		SampleRows: []notionMigrationSampleRow{},
	}
	// Of several, the first's properties are the database's and each
	// other's are read after it (notion_migration_scan_sources.go).
	if len(others) > 0 {
		entry.SourceName, entry.DataSources = first.Name, notionMigrationScanDataSources(others)
	}
	change.databases = append(change.databases, entry)
	for _, other := range others {
		change.queue = append(change.queue, notionMigrationScanItem{Kind: "source", ID: database.ID, Source: other.ID})
	}
	if len(sources) == 0 {
		change.queue = append(change.queue, notionMigrationScanItem{Kind: "rows", ID: database.ID})
	}
	for _, source := range sources {
		change.queue = append(change.queue, notionMigrationScanItem{Kind: "rows", ID: database.ID, Source: source})
	}
	change.queue = append(change.queue, notionMigrationScanItem{Kind: "views", ID: database.ID})
	return nil
}

func scanNotionMigrationRows(
	ctx context.Context, client notionProviderClient, store notionMigrationScanStore, change *notionMigrationScanChange,
	item notionMigrationScanItem,
) error {
	entry, found, err := store.database(item.ID)
	if err != nil || !found {
		return err
	}
	read := client.databaseRows
	if item.Source != "" {
		read = func(ctx context.Context, _ string, cursor string) (notionProviderPage, error) {
			return client.dataSourceRows(ctx, item.Source, cursor)
		}
	}
	listed, err := read(ctx, item.ID, item.Cursor)
	if err != nil {
		return err
	}
	rows, err := notionProviderPageResults(listed)
	if err != nil {
		return err
	}
	change.rows = len(rows)
	change.made.note(rows...)
	for _, row := range rows {
		notionMigrationCountScannedRow(&entry, item.Source, row)
		if len(entry.SampleRows) < notionMigrationSampleRows {
			entry.SampleRows = append(entry.SampleRows, notionMigrationSampleOf(row))
			change.queue = append(change.queue, notionMigrationScanItem{Kind: "rowbody", ID: row.ID, Depth: len(entry.SampleRows)})
		}
		if err := notionMigrationRowPeople(store, change, row); err != nil {
			return err
		}
		if err := notionMigrationAuthors(store, change, row); err != nil {
			return err
		}
		if seen, err := store.seen(row.ID); err != nil {
			return err
		} else if !seen {
			change.seen = append(change.seen, row.ID)
		}
	}
	change.updated = &entry
	if listed.HasMore && listed.NextCursor != nil {
		if entry.Rows >= notionMigrationRowCountCap {
			entry.RowsCapped = true
			return nil
		}
		more := item
		more.Cursor = *listed.NextCursor
		change.queue = append(change.queue, more)
	}
	return nil
}

// notionMigrationAuthors queues, once each, who made and last changed a row or
// page, a person or an integration: Notion names them by ID alone.
func notionMigrationAuthors(
	store notionMigrationScanStore, change *notionMigrationScanChange, object notionProviderObject,
) error {
	for _, user := range []*notionProviderUser{object.CreatedBy, object.LastEditedBy} {
		if user == nil || strings.TrimSpace(user.ID) == "" {
			continue
		}
		if err := notionMigrationQueuePerson(store, change, strings.TrimSpace(user.ID)); err != nil {
			return err
		}
	}
	return nil
}

func notionMigrationQueuePerson(store notionMigrationScanStore, change *notionMigrationScanChange, id string) error {
	key := "person:" + id
	if slices.Contains(change.seen, key) {
		return nil
	}
	seen, err := store.seen(key)
	if err != nil || seen {
		return err
	}
	change.seen = append(change.seen, key)
	change.queue = append(change.queue, notionMigrationScanItem{Kind: "person", ID: id})
	return nil
}

// scanNotionMigrationPerson reads a person or integration Notion named by ID;
// one Notion won't describe is kept by ID.
func scanNotionMigrationPerson(
	ctx context.Context, client notionProviderClient, change *notionMigrationScanChange, item notionMigrationScanItem,
) error {
	user := notionProviderUser{}
	err := client.request(ctx, http.MethodGet, "/v1/users/"+url.PathEscape(item.ID), nil, &user)
	if err != nil && !notionProviderRefused(err) {
		return err
	}
	person := notionMigrationPerson{ID: item.ID, Type: notionMigrationPersonType(user)}
	if user.Name != nil {
		person.Name = *user.Name
	}
	if email, ok := normalizedNotionEmail(user.Person.Email).(string); ok {
		person.Email = email
	}
	change.people = append(change.people, person)
	return nil
}

// notionMigrationBlockIssue reports each block type the import leaves out and each SVG file it cleans, one a page for each.
func notionMigrationBlockIssue(change *notionMigrationScanChange, pageID string, block notionProviderObject) {
	if notionMigrationBlockNotes(change, pageID, block); notionImportBlockTypeTaken(block.Type) {
		return
	}
	change.issues = append(change.issues, notionMigrationIssue{
		Code: notionImportIssueUnsupportedBlock, SourceID: pageID, Detail: block.Type,
	})
}

// notionMigrationMentionedPeople gathers the people a block's text mentions.
func notionMigrationMentionedPeople(change *notionMigrationScanChange, block notionProviderObject) {
	if len(block.Payload) == 0 {
		return
	}
	fields := struct {
		RichText []notionProviderRichText   `json:"rich_text"`
		Caption  []notionProviderRichText   `json:"caption"`
		Cells    [][]notionProviderRichText `json:"cells"`
	}{}
	if json.Unmarshal(block.Payload, &fields) != nil {
		return
	}
	for _, parts := range append([][]notionProviderRichText{fields.RichText, fields.Caption}, fields.Cells...) {
		for _, part := range parts {
			user := part.Mention.User
			if part.Type != "mention" || part.Mention.Type != "user" || strings.TrimSpace(user.ID) == "" ||
				user.Object == "bot" || user.Type == "bot" {
				continue
			}
			person := notionMigrationPerson{ID: strings.TrimSpace(user.ID)}
			if user.Name != nil {
				person.Name = *user.Name
			}
			if email, ok := normalizedNotionEmail(user.Person.Email).(string); ok {
				person.Email = email
			}
			change.people = append(change.people, person)
		}
	}
}

// notionMigrationRowPeople gathers the Notion people rows name, for a plan
// to map to the workspace's members; the store keeps each once.
func notionMigrationRowPeople(store notionMigrationScanStore, change *notionMigrationScanChange, row notionProviderObject) error {
	for _, property := range row.Properties {
		users := []notionProviderUser{}
		switch property.Type {
		case "people":
			_ = json.Unmarshal(property.Settings, &users)
		case "created_by", "last_edited_by":
			user := notionProviderUser{}
			if json.Unmarshal(property.Settings, &user) == nil {
				users = append(users, user)
			}
		}
		for _, user := range users {
			id := strings.TrimSpace(user.ID)
			if id == "" || user.Object == "bot" || user.Type == "bot" {
				continue
			}
			// One named by ID alone is read, as it may be an integration.
			if user.Name == nil {
				if err := notionMigrationQueuePerson(store, change, id); err != nil {
					return err
				}
				continue
			}
			person := notionMigrationPerson{ID: id, Name: *user.Name}
			if email, ok := normalizedNotionEmail(user.Person.Email).(string); ok {
				person.Email = email
			}
			change.people = append(change.people, person)
		}
	}
	return nil
}
