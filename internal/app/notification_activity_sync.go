package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errNotificationSyncInvalid       = errors.New("notification activity sync request is invalid")
	errNotificationSyncTargetChanged = errors.New("notification activity sync target changed")
)

const (
	notificationSyncTitleRunes   = 500
	notificationSyncPreviewRunes = 1000
	notificationSyncTargetRunes  = 2048
)

type normalizedNotificationActivity struct {
	notificationActivityInput
	OccurredAtMillis int64
}

type storedNotificationActivityPage struct {
	ID           string `db:"id"`
	Title        string `db:"title"`
	CreatedBy    string `db:"created_by"`
	LastEditedBy string `db:"last_edited_by"`
	UpdatedAt    string `db:"updated_at"`
}

type storedNotificationActivityBlock struct {
	ID           string `db:"id"`
	PageID       string `db:"page_id"`
	CreatedBy    string `db:"created_by"`
	LastEditedBy string `db:"last_edited_by"`
	UpdatedAt    int64  `db:"updated_at"`
}

type storedNotificationActivityReference struct {
	BlockID      string `db:"block_id"`
	TargetPageID string `db:"target_page_id"`
}

func syncNotificationActivities(
	ctx context.Context,
	app core.App,
	principalID string,
	workspaceID string,
	input notificationActivitySyncInput,
) (notificationActivitySyncResponse, error) {
	principalID = strings.TrimSpace(principalID)
	workspaceID = strings.TrimSpace(workspaceID)
	activities, err := normalizeNotificationActivities(input.Activities, principalID)
	if err != nil || principalID == "" || workspaceID == "" {
		return notificationActivitySyncResponse{}, errNotificationSyncInvalid
	}
	response := notificationActivitySyncResponse{}
	err = app.RunInTransaction(func(txApp core.App) error {
		pageIDs := notificationActivityPageIDs(activities)
		visible, err := visibleCommentNotificationPageSet(
			txApp, principalID, workspaceID, pageIDs,
		)
		if err != nil {
			return err
		}
		if len(visible) != len(pageIDs) {
			return errNotificationSyncTargetChanged
		}
		pages, err := loadNotificationActivityPages(ctx, txApp, workspaceID, activities)
		if err != nil {
			return err
		}
		blocks, err := loadNotificationActivityBlocks(ctx, txApp, workspaceID, activities)
		if err != nil {
			return err
		}
		references, err := loadNotificationActivityReferences(ctx, txApp, workspaceID, activities)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		projections := make([]productNotificationProjection, 0, len(activities))
		for _, activity := range activities {
			projection, ok := notificationActivityProjection(
				workspaceID, principalID, activity, pages, blocks, references, now,
			)
			if !ok {
				return errNotificationSyncTargetChanged
			}
			projections = append(projections, projection)
		}
		projections = newestNotificationActivityProjections(projections)
		if _, err := upsertProductNotifications(ctx, txApp, projections, now); err != nil {
			return err
		}
		keys := make([]string, 0, len(projections))
		for _, projection := range projections {
			keys = append(keys, projection.ActivityKey)
		}
		items, err := readSyncedProductNotifications(
			ctx, txApp, workspaceID, principalID, keys,
		)
		if err != nil {
			return err
		}
		response = notificationActivitySyncResponse{
			WorkspaceID: workspaceID, Items: items, AcceptedCount: len(projections),
		}
		return nil
	})
	return response, err
}

func normalizeNotificationActivities(
	inputs []notificationActivityInput,
	principalID string,
) ([]normalizedNotificationActivity, error) {
	if len(inputs) > notificationSyncActivitiesSize {
		return nil, errNotificationSyncInvalid
	}
	activities := make([]normalizedNotificationActivity, 0, len(inputs))
	for _, input := range inputs {
		input.ActivityKey = strings.TrimSpace(input.ActivityKey)
		input.Kind = strings.TrimSpace(input.Kind)
		input.PageID = strings.TrimSpace(input.PageID)
		input.BlockID = strings.TrimSpace(input.BlockID)
		input.TargetPageID = strings.TrimSpace(input.TargetPageID)
		input.ActorID = strings.TrimSpace(input.ActorID)
		input.Title = strings.TrimSpace(input.Title)
		input.Preview = strings.TrimSpace(input.Preview)
		input.Target = strings.TrimSpace(input.Target)
		input.OccurredAt = strings.TrimSpace(input.OccurredAt)
		occurredAt, err := time.Parse(time.RFC3339Nano, input.OccurredAt)
		if err != nil || occurredAt.UnixMilli() <= 0 ||
			!validCommentIdentifier(input.ActivityKey) ||
			!validCommentIdentifier(input.PageID) ||
			!validCommentIdentifier(input.ActorID) ||
			utf8.RuneCountInString(input.Title) > notificationSyncTitleRunes ||
			utf8.RuneCountInString(input.Preview) > notificationSyncPreviewRunes ||
			utf8.RuneCountInString(input.Target) > notificationSyncTargetRunes {
			return nil, errNotificationSyncInvalid
		}
		if input.ActorID == principalID {
			continue
		}
		switch input.Kind {
		case "page_edit":
			if input.BlockID != "" || input.TargetPageID != "" {
				return nil, errNotificationSyncInvalid
			}
		case "mention", "link":
			if !validCommentIdentifier(input.BlockID) ||
				!validCommentIdentifier(input.TargetPageID) {
				return nil, errNotificationSyncInvalid
			}
		default:
			return nil, errNotificationSyncInvalid
		}
		activities = append(activities, normalizedNotificationActivity{
			notificationActivityInput: input, OccurredAtMillis: occurredAt.UnixMilli(),
		})
	}
	return activities, nil
}

func notificationActivityPageIDs(activities []normalizedNotificationActivity) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(activities)*2)
	for _, activity := range activities {
		for _, id := range []string{activity.PageID, activity.TargetPageID} {
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

func notificationActivityBindings(
	prefix string,
	values []string,
	params dbx.Params,
) string {
	bindings := make([]string, len(values))
	for index, value := range values {
		key := prefix + strconv.Itoa(index)
		params[key] = value
		bindings[index] = "{:" + key + "}"
	}
	return joinSQL(bindings)
}

func notificationActivitySourceIDs(
	activities []normalizedNotificationActivity,
	selector func(normalizedNotificationActivity) string,
) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(activities))
	for _, activity := range activities {
		value := selector(activity)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func loadNotificationActivityPages(
	ctx context.Context,
	app core.App,
	workspaceID string,
	activities []normalizedNotificationActivity,
) (map[string]storedNotificationActivityPage, error) {
	ids := notificationActivitySourceIDs(activities, func(activity normalizedNotificationActivity) string {
		return activity.PageID
	})
	rows := []storedNotificationActivityPage{}
	if len(ids) > 0 {
		params := dbx.Params{"workspaceId": workspaceID}
		if err := app.DB().NewQuery(`
			SELECT [[id]], [[title]], [[created_by]], [[last_edited_by]],
				[[updated]] AS [[updated_at]]
			FROM {{leafen_pages}}
			WHERE [[workspace]]={:workspaceId} AND [[notion_import_staging]]=0
				AND [[in_trash]]=0 AND [[deletion_job_id]]=''
				AND [[id]] IN (` + notificationActivityBindings("page", ids, params) + `)
		`).WithContext(ctx).Bind(params).All(&rows); err != nil {
			return nil, err
		}
	}
	result := make(map[string]storedNotificationActivityPage, len(rows))
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, nil
}

func loadNotificationActivityBlocks(
	ctx context.Context,
	app core.App,
	workspaceID string,
	activities []normalizedNotificationActivity,
) (map[string]storedNotificationActivityBlock, error) {
	ids := notificationActivitySourceIDs(activities, func(activity normalizedNotificationActivity) string {
		return activity.BlockID
	})
	rows := []storedNotificationActivityBlock{}
	if len(ids) > 0 {
		params := dbx.Params{"workspaceId": workspaceID}
		if err := app.DB().NewQuery(`
			SELECT [[id]], [[page_id]], [[created_by]], [[last_edited_by]],
				[[updated]] AS [[updated_at]]
			FROM {{leafen_blocks}}
			WHERE [[workspace_id]]={:workspaceId}
				AND [[id]] IN (` + notificationActivityBindings("block", ids, params) + `)
		`).WithContext(ctx).Bind(params).All(&rows); err != nil {
			return nil, err
		}
	}
	result := make(map[string]storedNotificationActivityBlock, len(rows))
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, nil
}

func loadNotificationActivityReferences(
	ctx context.Context,
	app core.App,
	workspaceID string,
	activities []normalizedNotificationActivity,
) (map[string]struct{}, error) {
	blockIDs := notificationActivitySourceIDs(activities, func(activity normalizedNotificationActivity) string {
		return activity.BlockID
	})
	targetIDs := notificationActivitySourceIDs(activities, func(activity normalizedNotificationActivity) string {
		return activity.TargetPageID
	})
	rows := []storedNotificationActivityReference{}
	if len(blockIDs) > 0 && len(targetIDs) > 0 {
		params := dbx.Params{"workspaceId": workspaceID}
		if err := app.DB().NewQuery(`
			SELECT [[block_id]], [[target_page_id]]
			FROM {{leafen_block_page_references}}
			WHERE [[workspace_id]]={:workspaceId}
				AND [[block_id]] IN (` + notificationActivityBindings("referenceBlock", blockIDs, params) + `)
				AND [[target_page_id]] IN (` + notificationActivityBindings("referenceTarget", targetIDs, params) + `)
		`).WithContext(ctx).Bind(params).All(&rows); err != nil {
			return nil, err
		}
	}
	result := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		result[row.BlockID+"\x00"+row.TargetPageID] = struct{}{}
	}
	return result, nil
}

func notificationActivityProjection(
	workspaceID string,
	principalID string,
	activity normalizedNotificationActivity,
	pages map[string]storedNotificationActivityPage,
	blocks map[string]storedNotificationActivityBlock,
	references map[string]struct{},
	now int64,
) (productNotificationProjection, bool) {
	page, ok := pages[activity.PageID]
	if !ok {
		return productNotificationProjection{}, false
	}
	actorID := page.LastEditedBy
	if actorID == "" {
		actorID = page.CreatedBy
	}
	canonicalTarget := "/p/" + url.PathEscape(activity.PageID)
	canonicalKey := ""
	if activity.Kind == "page_edit" {
		occurredAt, ok := notificationActivityPageMillis(page.UpdatedAt)
		if !ok || occurredAt != activity.OccurredAtMillis {
			return productNotificationProjection{}, false
		}
		canonicalKey = notificationActivityCanonicalKey(activity, occurredAt)
	} else {
		block, blockOK := blocks[activity.BlockID]
		_, referenceOK := references[activity.BlockID+"\x00"+activity.TargetPageID]
		if !blockOK || !referenceOK || block.PageID != activity.PageID ||
			block.UpdatedAt != activity.OccurredAtMillis {
			return productNotificationProjection{}, false
		}
		actorID = block.LastEditedBy
		if actorID == "" {
			actorID = block.CreatedBy
		}
		canonicalTarget += "#block-" + url.PathEscape(activity.BlockID)
		canonicalKey = notificationActivityCanonicalKey(activity, block.UpdatedAt)
	}
	if actorID == "" || actorID != activity.ActorID || canonicalKey != activity.ActivityKey ||
		canonicalTarget != activity.Target {
		return productNotificationProjection{}, false
	}
	// An untitled page stays so, named in each reader's language where shown.
	title := strings.TrimSpace(page.Title)
	id := notificationActivityProjectionID("notification", workspaceID, principalID, canonicalKey)
	return productNotificationProjection{
		ID: id, EventID: notificationActivityProjectionID("event", workspaceID, principalID, canonicalKey),
		WorkspaceID: workspaceID, UserID: principalID, ActivityKey: canonicalKey,
		Kind: activity.Kind, PageID: activity.PageID, BlockID: activity.BlockID,
		ActorID: actorID, Title: title, Preview: activity.Preview, Target: canonicalTarget,
		OccurredAt: activity.OccurredAtMillis,
	}, true
}

func notificationActivityPageMillis(value string) (int64, bool) {
	for _, layout := range []string{
		time.RFC3339Nano, "2006-01-02 15:04:05.000Z", "2006-01-02 15:04:05Z",
	} {
		if parsed, err := time.Parse(layout, value); err == nil && parsed.UnixMilli() > 0 {
			return parsed.UnixMilli(), true
		}
	}
	return 0, false
}

func notificationActivityCanonicalKey(activity normalizedNotificationActivity, occurredAt int64) string {
	if activity.Kind == "page_edit" {
		return "page:" + activity.PageID + ":" + strconv.FormatInt(occurredAt, 10)
	}
	return strings.Join([]string{
		"reference", activity.BlockID, activity.TargetPageID, activity.Kind,
		strconv.FormatInt(occurredAt, 10),
	}, ":")
}

func notificationActivityProjectionID(namespace string, values ...string) string {
	digest := sha256.Sum256([]byte(namespace + "\x00" + strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:16])
}

func newestNotificationActivityProjections(
	projections []productNotificationProjection,
) []productNotificationProjection {
	positions := make(map[string]int, len(projections))
	result := make([]productNotificationProjection, 0, len(projections))
	for _, projection := range projections {
		if index, ok := positions[projection.ActivityKey]; ok {
			if projection.OccurredAt > result[index].OccurredAt {
				result[index] = projection
			}
			continue
		}
		positions[projection.ActivityKey] = len(result)
		result = append(result, projection)
	}
	return result
}

func readSyncedProductNotifications(
	ctx context.Context,
	app core.App,
	workspaceID string,
	principalID string,
	activityKeys []string,
) ([]notificationInboxRecord, error) {
	if len(activityKeys) == 0 {
		return []notificationInboxRecord{}, nil
	}
	params := dbx.Params{"workspaceId": workspaceID, "principalId": principalID}
	rows := []storedNotificationInboxRecord{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[activity_key]], [[kind]], [[page_id]], [[block_id]],
			[[comment_id]], [[actor_id]], [[title]], [[preview]], [[preview_spans]], [[target]],
			[[occurred_at]], [[read_at]], [[archived_at]], [[personal]], [[actor_connection]]
		FROM {{leafen_notifications}}
		WHERE [[workspace_id]]={:workspaceId} AND [[user_id]]={:principalId}
			AND [[archived_at]]=0
			AND [[activity_key]] IN (` + notificationActivityBindings("activity", activityKeys, params) + `)
		ORDER BY [[occurred_at]] DESC, [[id]] DESC
	`).WithContext(ctx).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	items := make([]notificationInboxRecord, 0, len(rows))
	for _, row := range rows {
		items = append(items, notificationInboxRecord{
			ID: row.ID, ActivityKey: row.ActivityKey, Kind: row.Kind,
			PageID: row.PageID, BlockID: row.BlockID, CommentID: row.CommentID,
			ActorID: row.ActorID, Title: row.Title, Preview: row.Preview, PreviewSpans: notificationPreviewSpans(row.PreviewSpans),
			Target: row.Target, OccurredAt: notificationInboxTime(row.OccurredAt),
			ReadAt: notificationInboxTime(row.ReadAt), Personal: row.Personal,
			ActorConnection: noticeKeptConnection(row.ActorConnection),
		})
	}
	notificationInboxItemsNow(app, principalID, items)
	return items, nil
}
