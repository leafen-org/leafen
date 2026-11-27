package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The restructure steps that keep what the plan doesn't give a place: a
// reported property's values go to the run's report and the property to
// the database's Deleted properties; Notion's comments come as comments on
// their pages, each marked with who wrote it and when; and the plan's
// people take the Notion people the import named.

// notionMigrationCommentPagesPerTurn bounds the pages a turn reads
// comments for, and notionMigrationCommentBlocksPerTurn the blocks.
const (
	notionMigrationCommentPagesPerTurn  = 10
	notionMigrationCommentBlocksPerTurn = 50
)

// notionMigrationCommentSourcesSQL keeps the mappings whose source is a page
// or block Notion gave. A part the import made of one — a database's top
// page, a callout's first line (`#callout-first-line`) — has no comments of
// its own, and Notion refuses its ID (400, seen 2026-11-13: a real run's one
// callout ended the comments of every block after it).
const notionMigrationCommentSourcesSQL = `[[mapping]].[[source_id]] NOT LIKE 'notion-database-root-page:%'
			AND INSTR([[mapping]].[[source_id]], '#') = 0`

// errNotionMigrationCommentsWithheld is Notion keeping every comment from
// the connection (403: the connection may not read comments).
var errNotionMigrationCommentsWithheld = errors.New("notion migration comments withheld")

// readNotionMigrationComments reads a page's or a block's comments, and
// whether Notion gave them: one it refuses (400, 404 — a block Notion won't
// list comments for, one deleted since) is passed by and counted, the rest
// still asked.
func readNotionMigrationComments(ctx context.Context, client notionProviderClient, id string) ([]notionProviderComment, bool, error) {
	comments, cursor := []notionProviderComment{}, ""
	for {
		listed, err := client.comments(ctx, id, cursor)
		var status *notionProviderStatusError
		if errors.As(err, &status) && status.status == http.StatusForbidden {
			return nil, false, errNotionMigrationCommentsWithheld
		}
		if notionProviderRefused(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		comments = append(comments, listed.Results...)
		if !listed.HasMore || listed.NextCursor == nil {
			return comments, true, nil
		}
		cursor = *listed.NextCursor
	}
}

// reportNotionMigrationProperty moves a reported property to Deleted
// properties, where every value stays until a person deletes it (one a view
// still groups or dates by stays where it is), and records in the run's
// report how many values it has, where they are, and the first of them.
func reportNotionMigrationProperty(app core.App, job storedJob, payload notionImportJobPayload, step notionMigrationStep) error {
	dataSourceID, err := notionImportMappedID(app, job.ID, step.Database.SourceID, "data_source")
	if err != nil || dataSourceID == "" {
		return err
	}
	nativeID, err := notionImportMappedID(app, job.ID, notionPropertySourceID(step.Database.SourceID, step.SourceID), "property")
	if err != nil || nativeID == "" {
		return err
	}
	pageID, err := notionMigrationDataSourcePage(app, dataSourceID)
	if err != nil {
		return err
	}
	stored, exists, err := loadDatabasePropertyRecord(app, pageID, nativeID)
	if err != nil || !exists {
		return err
	}
	reported := notionMigrationReportedProperty{
		DatabaseID: step.Database.SourceID, PropertyID: step.SourceID, Name: stored.Name, Type: stored.Type,
		Values: []notionMigrationReportedValue{},
	}
	if formula, ok := stored.Config["formula"].(string); ok {
		// A computed property's values come from its definition.
		reported.Values = append(reported.Values, notionMigrationReportedValue{Value: formula})
		reported.Kept = "formula"
	}
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_row_values}} [[value]]
		JOIN {{leafen_database_rows}} [[row]] ON [[row]].[[id]] = [[value]].[[row_id]]
		WHERE [[row]].[[data_source_id]] = {:dataSourceId} AND [[value]].[[property_id]] = {:propertyId}
			AND [[value]].[[value]] NOT IN ('', 'null', '""', '[]', '{}')
	`).Bind(dbx.Params{"dataSourceId": dataSourceID, "propertyId": nativeID}).Row(&reported.Total); err != nil {
		return err
	}
	rows := []struct {
		ID    string `db:"id"`
		Title string `db:"title"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[title]] FROM {{leafen_pages}} WHERE [[data_source_id]]={:dataSourceId}
		ORDER BY [[position]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"dataSourceId": dataSourceID, "limit": notionMigrationReportedValues + 1}).All(&rows); err != nil {
		return err
	}
	if len(rows) > notionMigrationReportedValues {
		rows, reported.Truncated = rows[:notionMigrationReportedValues], true
	}
	conversion, convertible := (*databasePropertyConversion)(nil), false
	if _, isStored := databasePropertyConversionStoredTypes[stored.Type]; isStored {
		conversion, err = newDatabasePropertyConversion(reported.Type, graphValueTypeRichText, stored.Config, nil,
			databasePropertyConversionNames{Person: databasePropertyConversionPersonName(app)})
		convertible = err == nil
	}
	if convertible && len(rows) > 0 {
		rowIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			rowIDs = append(rowIDs, row.ID)
		}
		cells, err := loadGraphRowValues(app, payload.AuthorityOwnerID, pageID, rowIDs)
		if err != nil {
			return err
		}
		values := map[string]string{}
		for _, cell := range cells {
			if cell.PropertyID != nativeID {
				continue
			}
			if text, err := conversion.sourceText(json.RawMessage(cell.Value)); err == nil && strings.TrimSpace(text) != "" {
				values[cell.RowID] = notionMigrationValueReportText(text)
			}
		}
		for _, row := range rows {
			if value, ok := values[row.ID]; ok {
				reported.Values = append(reported.Values, notionMigrationReportedValue{RowID: row.ID, RowTitle: row.Title, Value: value})
			}
		}
	}
	// The property goes to Deleted properties first, so the report says
	// where its values are; a replay finds it there already.
	target, err := loadDatabasePropertyTrashTarget(app, pageID, nativeID)
	if err != nil {
		return err
	}
	if reported.Kept == "" {
		reported.Kept = "deleted_property"
	}
	if target.DeletedAt == 0 {
		if err := trashDatabaseProperty(app, target, job.Owner, time.Now().UTC()); errors.Is(err, errDatabasePropertyInUse) {
			reported.Kept = "property"
		} else if err != nil {
			return err
		}
	}
	return updateNotionMigrationRunReport(app, payload.MigrationID, func(report *notionMigrationRunReport) {
		for _, existing := range report.Reported {
			if existing.DatabaseID == reported.DatabaseID && existing.PropertyID == reported.PropertyID {
				return
			}
		}
		report.Reported = append(report.Reported, reported)
	})
}

func notionMigrationValueReportText(value string) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= notionMigrationValueRunes {
		return value
	}
	return string([]rune(value)[:notionMigrationValueRunes])
}

type notionProviderComment struct {
	ID           string                   `json:"id"`
	DiscussionID string                   `json:"discussion_id"`
	CreatedTime  string                   `json:"created_time"`
	CreatedBy    notionProviderUser       `json:"created_by"`
	RichText     []notionProviderRichText `json:"rich_text"`
	DisplayName  struct {
		ResolvedName string `json:"resolved_name"`
	} `json:"display_name"`
}

type notionProviderCommentPage struct {
	Results    []notionProviderComment `json:"results"`
	HasMore    bool                    `json:"has_more"`
	NextCursor *string                 `json:"next_cursor"`
}

func (client notionProviderClient) comments(ctx context.Context, blockID, cursor string) (notionProviderCommentPage, error) {
	query := url.Values{"block_id": {blockID}, "page_size": {"100"}}
	if cursor != "" {
		query.Set("start_cursor", cursor)
	}
	var page notionProviderCommentPage
	err := client.request(ctx, http.MethodGet, "/v1/comments?"+query.Encode(), nil, &page)
	return page, err
}

// importNotionMigrationComments brings a turn's pages' Notion comments and
// reports whether pages remain. Comments Notion won't give the connection
// are noted in the report and the rest passed by; a page or block whose
// comments Notion refuses is counted in the report and the next one asked,
// and so is one a person took out of reach meanwhile
// (notion_import_changed_meanwhile.go).
func (r *durableJobRunner) importNotionMigrationComments(
	ctx context.Context, job storedJob, payload *notionImportJobPayload, people func(string) string,
) (bool, error) {
	connection, err := loadNotionConnection(r.app, payload.ConnectionID, job.Owner, job.WorkspaceID)
	if err != nil {
		return false, err
	}
	token, err := decryptNotionConnection(r.app, connection)
	if err != nil {
		return false, err
	}
	client := r.notionProviderClient(payload.ConnectionID, token)
	members, err := notionMigrationPlanMembers(r.app, payload.MigrationID)
	if err != nil {
		return false, err
	}
	_, err = loadBundleSource(r.app, job.Owner, job.WorkspaceID, payload.ConnectionID)
	if err != nil && !errors.Is(err, errBundleSourceNotFound) {
		return false, err
	}
	fromBundle := err == nil
	if payload.CommentBlocks {
		return r.importNotionMigrationBlockComments(ctx, job, payload, client, people, members, fromBundle)
	}
	// Only a page this run's source gave has comments to ask for; one an
	// earlier run moved that the source no longer has would be refused.
	from := `
		FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]] IN ('page','row') AND [[mapping]].[[source_id]]>{:cursor}
			AND ` + notionMigrationCommentSourcesSQL + `
			AND EXISTS(SELECT 1 FROM {{leafen_notion_import_sources}} AS [[source]]
				WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[source_id]]=[[mapping]].[[source_id]])` +
		notionMigrationCommentsAskedSQL
	params := dbx.Params{
		"jobId": job.ID, "cursor": payload.CommentCursor, "limit": notionMigrationCommentPagesPerTurn + 1,
		"migrationId": payload.MigrationID, "askedSince": payload.CommentsAskedSince,
	}
	if payload.CommentCursor == "" {
		payload.CommentPagesDone = 0
		// How many it asks, as the blocks' are counted: its line and its time
		// left say the pages asked of all (2026-11-20: the comments' line
		// stood with no count while the pages and rows were asked). The count
		// is kept by a turn of its own, so it shows before the first pages
		// are asked, a turn later.
		counted := payload.CommentPagesTotal > 0
		if err := r.app.DB().NewQuery(`SELECT COUNT(*) ` + from).Bind(params).Row(&payload.CommentPagesTotal); err != nil {
			return false, err
		}
		if !counted && payload.CommentPagesTotal > 0 {
			return true, nil
		}
		// Each run asks again whether the connection gives comments, and
		// counts the comments it moves; one that takes over a stopped run
		// goes on counting the stopped run's (notion_migration_run_again.go).
		if err := updateNotionMigrationRunReport(r.app, payload.MigrationID, func(report *notionMigrationRunReport) {
			report.CommentsUnavailable = false
			if payload.CommentsAskedSince <= 0 {
				report.CommentsImported, report.CommentsUnreadable = 0, 0
			}
		}); err != nil {
			return false, err
		}
	}
	pages := []struct {
		SourceID string `db:"source_id"`
		NativeID string `db:"native_id"`
		Kind     string `db:"kind"`
	}{}
	if err := r.app.DB().NewQuery(`
		SELECT [[mapping]].[[source_id]],[[mapping]].[[native_id]],[[mapping]].[[kind]]` + from + `
		ORDER BY [[mapping]].[[source_id]] LIMIT {:limit}
	`).Bind(params).All(&pages); err != nil {
		return false, err
	}
	remaining := len(pages) > notionMigrationCommentPagesPerTurn
	if remaining {
		pages = pages[:notionMigrationCommentPagesPerTurn]
	}
	imported, unreadable, passed := 0, 0, 0
	asked := make([]string, 0, len(pages))
	ids := make([]string, len(pages))
	for index, page := range pages {
		ids[index] = page.SourceID
	}
	answers := askNotionMigrationComments(ctx, client, ids, func(index int) {
		noteNotionMigrationLivePage(r.app, payload.MigrationID, notionMigrationStageComments, pages[index].Kind, pages[index].NativeID)
	})
	for index, page := range pages {
		comments, readable, err := answers[index].comments, answers[index].readable, answers[index].err
		if errors.Is(err, errNotionMigrationCommentsWithheld) {
			return false, updateNotionMigrationRunReport(r.app, payload.MigrationID, func(report *notionMigrationRunReport) {
				report.CommentsUnavailable = true
			})
		}
		// Notion asked to wait, or the server stops: the turn keeps what came
		// before and ends there (notion_migration_comment_asks.go).
		if end, failed := notionMigrationCommentsTurnEnd(ctx, payload, index, notionImportAt(page.Kind, page.NativeID, err)); end {
			remaining = true
			break
		} else if failed != nil {
			return false, failed
		}
		if !readable {
			unreadable++
		}
		count, err := createNotionMigrationComments(r.app, job, payload.MigrationID, page.NativeID, "", page.Kind == "row", comments, people,
			members, fromBundle)
		if notionImportChangedMeanwhile(err) {
			passed++
		} else if err != nil {
			return false, notionImportAt(page.Kind, page.NativeID, err)
		}
		imported += count
		payload.CommentCursor = page.SourceID
		payload.CommentPagesDone++
		asked = append(asked, page.SourceID)
	}
	// A run after this one, should this one stop, doesn't ask them again.
	if err := recordNotionMigrationCommentsAsked(r.app, payload.MigrationID, asked); err != nil {
		return false, err
	}
	// Notion lists a page's comments by the page and a block's by the block,
	// and a comment changes no edit time (seen 2026-11-07), so every run asks
	// each block again once the pages are done — but for those a stopped run
	// of the same move asked already (notionMigrationCommentsAskedSQL).
	payload.CommentBlocks = !remaining
	return true, updateNotionMigrationRunReport(r.app, payload.MigrationID, func(report *notionMigrationRunReport) {
		report.CommentsImported += imported
		report.CommentsUnreadable += unreadable
		report.ChangedMeanwhile += passed
	})
}

// importNotionMigrationBlockComments brings a turn's blocks' Notion comments
// to their blocks and reports whether blocks remain.
func (r *durableJobRunner) importNotionMigrationBlockComments(
	ctx context.Context, job storedJob, payload *notionImportJobPayload, client notionProviderClient,
	people func(string) string, members map[string]string, fromBundle bool,
) (bool, error) {
	blocks := []struct {
		SourceID string `db:"source_id"`
		NativeID string `db:"native_id"`
		PageID   string `db:"page_id"`
		Row      bool   `db:"row"`
	}{}
	from := `
		FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]]=[[mapping]].[[native_id]]
		WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]]='block' AND [[mapping]].[[source_id]]>{:cursor}
			AND ` + notionMigrationCommentSourcesSQL + `
			AND EXISTS(SELECT 1 FROM {{leafen_notion_import_sources}} AS [[source]]
				WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[source_id]]=[[mapping]].[[source_id]])` +
		notionMigrationDiscussionsSQL + notionMigrationCommentsAskedSQL
	// Only the blocks the reading of the person's Notion saw comments on,
	// where it read the body (notion_migration_comment_discussions.go).
	params, err := notionMigrationCommentAsking(r.app, payload.MigrationID)
	if err != nil {
		return false, err
	}
	params["jobId"], params["migrationId"], params["askedSince"] = job.ID, payload.MigrationID, payload.CommentsAskedSince
	if payload.CommentBlockCursor == "" {
		payload.CommentBlocksDone = 0
		params["cursor"] = ""
		// Kept by a turn of its own, as the pages' count is.
		counted := payload.CommentBlocksTotal > 0
		if err := r.app.DB().NewQuery(`SELECT COUNT(*) ` + from).Bind(params).Row(&payload.CommentBlocksTotal); err != nil {
			return false, err
		}
		if !counted && payload.CommentBlocksTotal > 0 {
			return true, nil
		}
	}
	// A block of a database page is the row's, under the row's ID.
	params["cursor"], params["limit"] = payload.CommentBlockCursor, notionMigrationCommentBlocksPerTurn+1
	if err := r.app.DB().NewQuery(`
		SELECT [[mapping]].[[source_id]], [[mapping]].[[native_id]], [[block]].[[page_id]],
			EXISTS(SELECT 1 FROM {{leafen_database_row_pages}} AS [[row]] WHERE [[row]].[[row_id]]=[[block]].[[page_id]]) AS [[row]]
		` + from + `
		ORDER BY [[mapping]].[[source_id]] LIMIT {:limit}
	`).Bind(params).All(&blocks); err != nil {
		return false, err
	}
	remaining := len(blocks) > notionMigrationCommentBlocksPerTurn
	if remaining {
		blocks = blocks[:notionMigrationCommentBlocksPerTurn]
	}
	imported, unreadable, passed := 0, 0, 0
	asked := make([]string, 0, len(blocks))
	ids := make([]string, len(blocks))
	for index, block := range blocks {
		ids[index] = block.SourceID
	}
	answers := askNotionMigrationComments(ctx, client, ids, func(index int) {
		noteNotionMigrationLivePage(r.app, payload.MigrationID, notionMigrationStageComments, map[bool]string{true: "row", false: "page"}[blocks[index].Row], blocks[index].PageID)
	})
	for index, block := range blocks {
		comments, readable, err := answers[index].comments, answers[index].readable, answers[index].err
		if errors.Is(err, errNotionMigrationCommentsWithheld) {
			return false, updateNotionMigrationRunReport(r.app, payload.MigrationID, func(report *notionMigrationRunReport) {
				report.CommentsUnavailable = true
			})
		}
		// Notion asked to wait, or the server stops: the turn keeps what came
		// before and ends there (notion_migration_comment_asks.go).
		if end, failed := notionMigrationCommentsTurnEnd(ctx, payload, index, notionImportAt("block", block.NativeID, err)); end {
			remaining = true
			break
		} else if failed != nil {
			return false, failed
		}
		if !readable {
			unreadable++
		}
		if len(comments) > 0 {
			count, err := createNotionMigrationComments(r.app, job, payload.MigrationID, block.PageID, block.NativeID, block.Row,
				comments, people, members, fromBundle)
			if notionImportChangedMeanwhile(err) {
				passed++
			} else if err != nil {
				return false, notionImportAt("block", block.NativeID, err)
			}
			imported += count
		}
		payload.CommentBlockCursor = block.SourceID
		payload.CommentBlocksDone++
		asked = append(asked, block.SourceID)
	}
	if err := recordNotionMigrationCommentsAsked(r.app, payload.MigrationID, asked); err != nil {
		return false, err
	}
	return remaining, updateNotionMigrationRunReport(r.app, payload.MigrationID, func(report *notionMigrationRunReport) {
		report.CommentsImported += imported
		report.CommentsUnreadable += unreadable
		report.ChangedMeanwhile += passed
	})
}

// mapNotionMigrationPeople has the plan's people take the Notion people the
// import named, through the imported people mapping, a turn at a time, and
// reports whether the mapping goes on.
func (r *durableJobRunner) mapNotionMigrationPeople(job storedJob, payload *notionImportJobPayload, step notionMigrationStep) (bool, error) {
	if r.ygo == nil {
		return false, errNotionMigrationRestructure
	}
	mappings := make(map[string]string, len(step.People))
	for notionUserID, memberID := range step.People {
		mappings["notion-user:"+notionUserID] = memberID
	}
	requestID := "notion-migration-" + job.ID + "-people-" + step.TargetID
	// Those the plan invites are invited first, and their values name them.
	err := inviteNotionMigrationPeople(r.app, job, mappings)
	result := notionImportedPeopleApplyResult{}
	if err == nil {
		result, err = applyImportedPeopleMappingTurn(r.app, r.ygo, job.Owner, job.WorkspaceID, notionImportedPeopleApplyInput{
			RequestID: requestID, Mappings: mappings, Continuation: payload.PeopleContinuation,
		})
	}
	if err != nil {
		// The mapping covers the workspace's imported pages, not only this
		// run's; what stops it is reported, and it can be done again from
		// the import setting — let go of, so that needn't wait — while what
		// moved stays.
		payload.PeopleContinuation = ""
		if releaseErr := releaseImportedPeopleMappingJob(r.app, job.Owner, requestID); releaseErr != nil {
			return false, releaseErr
		}
		code := errorCodeImportedPeopleMappingFailed
		if isPermissionDenied(err) {
			code = errorCodePermissionDenied
		}
		return false, updateNotionMigrationRunReport(r.app, payload.MigrationID, func(report *notionMigrationRunReport) {
			report.PeopleError = code
		})
	}
	if result.Status != "completed" {
		payload.PeopleContinuation = result.Continuation
		return true, nil
	}
	payload.PeopleContinuation = ""
	return false, updateNotionMigrationRunReport(r.app, payload.MigrationID, func(report *notionMigrationRunReport) {
		report.PeopleMapped += result.MappedPeople
		report.PeopleSkippedPages += result.SkippedPages
	})
}
