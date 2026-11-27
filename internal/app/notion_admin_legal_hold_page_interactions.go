package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	pageInteractionCreated         = "page.created"
	pageInteractionEdited          = "page.edited"
	pageInteractionViewed          = "page.viewed"
	pageInteractionReadWindowLimit = 64
	pageInteractionWriteBatchLimit = 256
)

type pageInteraction struct {
	EventID         string `db:"event_id"`
	OrganizationID  string `db:"organization_id"`
	WorkspaceID     string `db:"workspace_id"`
	PageID          string `db:"page_id"`
	UserID          string `db:"user_id"`
	InteractionType string `db:"interaction_type"`
	OccurredAt      int64  `db:"occurred_at"`
	ExpiresAt       int64  `db:"expires_at"`
}

type pageInteractionWindow struct {
	Items   []pageInteraction
	HasMore bool
}

type pageInteractionScope struct {
	OrganizationID string `db:"organization_id"`
	WorkspaceID    string `db:"workspace_id"`
}

func pageInteractionEventID(parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		_, _ = digest.Write([]byte(part))
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func pageCreatedInteraction(
	organizationID, workspaceID, pageID, userID string,
	occurredAt int64,
) pageInteraction {
	return pageInteraction{
		EventID:        pageInteractionEventID(pageInteractionCreated, pageID),
		OrganizationID: organizationID, WorkspaceID: workspaceID,
		PageID: pageID, UserID: userID, InteractionType: pageInteractionCreated,
		OccurredAt: occurredAt,
	}
}

func pageEditedInteraction(
	source, identity, organizationID, workspaceID, pageID, userID string,
	occurredAt int64,
) pageInteraction {
	return pageInteraction{
		EventID:        pageInteractionEventID(pageInteractionEdited, source, identity),
		OrganizationID: organizationID, WorkspaceID: workspaceID,
		PageID: pageID, UserID: userID, InteractionType: pageInteractionEdited,
		OccurredAt: occurredAt,
	}
}

func pageViewedInteractions(
	batchID, organizationID, workspaceID, userID string,
	pageIDs []string,
	occurredAt time.Time,
) []pageInteraction {
	result := make([]pageInteraction, 0, len(pageIDs))
	expiresAt := occurredAt.UTC().AddDate(0, 0, maxPageAnalyticsReadDays).UnixMilli()
	for _, pageID := range pageIDs {
		result = append(result, pageInteraction{
			EventID:        pageInteractionEventID(pageInteractionViewed, workspaceID, batchID, pageID),
			OrganizationID: organizationID, WorkspaceID: workspaceID,
			PageID: pageID, UserID: userID, InteractionType: pageInteractionViewed,
			OccurredAt: occurredAt.UTC().UnixMilli(), ExpiresAt: expiresAt,
		})
	}
	return result
}

func recordNotionAdminLegalHoldPageInteraction(
	app core.App,
	interactions ...pageInteraction,
) error {
	if len(interactions) == 0 {
		return nil
	}
	if len(interactions) > pageInteractionWriteBatchLimit {
		return errors.New("page interaction batch exceeds the bounded owner")
	}
	workspaceID := interactions[0].WorkspaceID
	seen := make(map[string]struct{}, len(interactions))
	params := dbx.Params{}
	values := make([]string, 0, len(interactions))
	hasViewed := false
	for index, interaction := range interactions {
		if !validPageInteraction(interaction) || interaction.WorkspaceID != workspaceID {
			return errors.New("page interaction is invalid")
		}
		if _, duplicate := seen[interaction.EventID]; duplicate {
			return errors.New("page interaction batch contains duplicate identity")
		}
		seen[interaction.EventID] = struct{}{}
		hasViewed = hasViewed || interaction.InteractionType == pageInteractionViewed
		prefix := fmt.Sprintf("interaction%d", index)
		params[prefix+"Event"] = interaction.EventID
		params[prefix+"Organization"] = interaction.OrganizationID
		params[prefix+"Workspace"] = interaction.WorkspaceID
		params[prefix+"Page"] = interaction.PageID
		params[prefix+"User"] = interaction.UserID
		params[prefix+"Type"] = interaction.InteractionType
		params[prefix+"Occurred"] = interaction.OccurredAt
		params[prefix+"Expires"] = interaction.ExpiresAt
		values = append(values, fmt.Sprintf(
			"({:%sEvent},{:%sOrganization},{:%sWorkspace},{:%sPage},{:%sUser},{:%sType},{:%sOccurred},{:%sExpires})",
			prefix, prefix, prefix, prefix, prefix, prefix, prefix, prefix,
		))
	}
	inserted := []struct {
		EventID string `db:"event_id"`
	}{}
	err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_interactions}} (
			[[event_id]], [[organization_id]], [[workspace_id]], [[page_id]],
			[[user_id]], [[interaction_type]], [[occurred_at]], [[expires_at]]
		) VALUES ` + strings.Join(values, ",") + `
		ON CONFLICT ([[event_id]]) DO NOTHING
		RETURNING [[event_id]]
	`).Bind(params).All(&inserted)
	if err != nil {
		return err
	}
	if err := verifyPageInteractionIdentities(app, interactions); err != nil {
		return err
	}
	if len(inserted) > 0 {
		if err := bumpPageInteractionRevisionBy(
			app, map[string]int{workspaceID: len(inserted)},
		); err != nil {
			return err
		}
		if hasViewed {
			notifyPageInteractionRetentionAfterCommit(app)
		}
	}
	return nil
}

func validPageInteraction(value pageInteraction) bool {
	if value.EventID == "" || len(value.EventID) > 255 || value.OrganizationID == "" ||
		value.WorkspaceID == "" || value.PageID == "" || value.UserID == "" || value.OccurredAt <= 0 {
		return false
	}
	if value.InteractionType == pageInteractionViewed {
		return value.ExpiresAt > value.OccurredAt
	}
	return (value.InteractionType == pageInteractionCreated || value.InteractionType == pageInteractionEdited) &&
		value.ExpiresAt == 0
}

func verifyPageInteractionIdentities(app core.App, expected []pageInteraction) error {
	params := dbx.Params{}
	bindings := make([]string, len(expected))
	for index, interaction := range expected {
		key := fmt.Sprintf("event%d", index)
		params[key] = interaction.EventID
		bindings[index] = "{:" + key + "}"
	}
	stored := []pageInteraction{}
	if err := app.DB().NewQuery(`
		SELECT [[event_id]], [[organization_id]], [[workspace_id]], [[page_id]],
			[[user_id]], [[interaction_type]], [[occurred_at]], [[expires_at]]
		FROM {{leafen_page_interactions}}
		WHERE [[event_id]] IN (` + strings.Join(bindings, ",") + `)
		ORDER BY [[event_id]]
	`).Bind(params).All(&stored); err != nil {
		return err
	}
	ordered := append([]pageInteraction(nil), expected...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].EventID < ordered[right].EventID })
	if fmt.Sprint(stored) != fmt.Sprint(ordered) {
		return errors.New("page interaction identity was reused")
	}
	return nil
}

func bumpPageInteractionRevisionBy(app core.App, increments map[string]int) error {
	if len(increments) == 0 || len(increments) > pageInteractionWriteBatchLimit {
		return errors.New("page interaction revision batch is invalid")
	}
	workspaceIDs := make([]string, 0, len(increments))
	for workspaceID, increment := range increments {
		if workspaceID == "" || increment < 1 {
			return errors.New("page interaction revision increment is invalid")
		}
		workspaceIDs = append(workspaceIDs, workspaceID)
	}
	sort.Strings(workspaceIDs)
	params := dbx.Params{}
	values := make([]string, len(workspaceIDs))
	for index, workspaceID := range workspaceIDs {
		workspaceKey := fmt.Sprintf("workspace%d", index)
		incrementKey := fmt.Sprintf("increment%d", index)
		params[workspaceKey] = workspaceID
		params[incrementKey] = increments[workspaceID]
		values[index] = fmt.Sprintf("({:%s},{:%s})", workspaceKey, incrementKey)
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_interaction_revisions}} ([[workspace_id]], [[revision]])
		VALUES ` + strings.Join(values, ",") + `
		ON CONFLICT ([[workspace_id]]) DO UPDATE
		SET [[revision]] = [[revision]] + excluded.[[revision]]
	`).Bind(params).Execute()
	return err
}

func initializePageInteractionRevision(app core.App, workspaceID string) error {
	if workspaceID == "" {
		return errors.New("page interaction revision workspace is required")
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_interaction_revisions}} ([[workspace_id]], [[revision]])
		VALUES ({:workspaceId}, 0)
		ON CONFLICT ([[workspace_id]]) DO NOTHING
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Execute()
	return err
}

func readPageInteractionRevision(app core.App, workspaceID string) (int, error) {
	var revision int
	err := app.DB().NewQuery(`
		SELECT [[revision]] FROM {{leafen_page_interaction_revisions}}
		WHERE [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&revision)
	return revision, err
}

func loadPageInteractionScope(app core.App, pageID string) (pageInteractionScope, error) {
	rows := []pageInteractionScope{}
	err := app.DB().NewQuery(`
		SELECT [[workspace]].[[organization]] AS [[organization_id]],
			[[page]].[[workspace]] AS [[workspace_id]]
		FROM {{leafen_pages}} [[page]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[page]].[[workspace]]
		WHERE [[page]].[[id]] = {:pageId}
		LIMIT 2
	`).Bind(dbx.Params{"pageId": pageID}).All(&rows)
	if err != nil {
		return pageInteractionScope{}, err
	}
	if len(rows) != 1 {
		return pageInteractionScope{}, errors.New("page interaction scope is ambiguous")
	}
	return rows[0], nil
}

func listPageInteractionWindow(
	app core.App,
	workspaceID string,
	userIDs, interactionTypes []string,
	startAt, endAt int64,
	afterAt int64,
	afterEventID string,
) (pageInteractionWindow, error) {
	if workspaceID == "" || len(userIDs) == 0 ||
		len(interactionTypes) == 0 || len(interactionTypes) > 3 || startAt < 0 || endAt < startAt || afterAt < 0 {
		return pageInteractionWindow{}, errors.New("page interaction read is invalid")
	}
	params := dbx.Params{
		"workspaceId": workspaceID, "startAt": startAt, "endAt": endAt,
		"afterAt": afterAt, "afterEventId": afterEventID,
		"limit": pageInteractionReadWindowLimit + 1,
	}
	// However many people, as one JSON list.
	encodedUsers, err := json.Marshal(userIDs)
	if err != nil {
		return pageInteractionWindow{}, err
	}
	params["users"] = string(encodedUsers)
	types := bindPageInteractionValues(params, "type", interactionTypes)
	items := []pageInteraction{}
	err = app.DB().NewQuery(`
		SELECT [[event_id]], [[organization_id]], [[workspace_id]], [[page_id]],
			[[user_id]], [[interaction_type]], [[occurred_at]], [[expires_at]]
		FROM {{leafen_page_interactions}}
		WHERE [[workspace_id]] = {:workspaceId}
			AND [[user_id]] IN (SELECT [[value]] FROM json_each({:users}))
			AND [[interaction_type]] IN (` + strings.Join(types, ",") + `)
			AND [[occurred_at]] >= {:startAt} AND [[occurred_at]] <= {:endAt}
			AND ([[occurred_at]] > {:afterAt}
				OR ([[occurred_at]] = {:afterAt} AND [[event_id]] > {:afterEventId}))
		ORDER BY [[occurred_at]], [[event_id]]
		LIMIT {:limit}
	`).Bind(params).All(&items)
	if err != nil {
		return pageInteractionWindow{}, err
	}
	hasMore := len(items) > pageInteractionReadWindowLimit
	if hasMore {
		items = items[:pageInteractionReadWindowLimit]
	}
	return pageInteractionWindow{Items: items, HasMore: hasMore}, nil
}

func bindPageInteractionValues(params dbx.Params, prefix string, values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		key := fmt.Sprintf("%s%d", prefix, index)
		params[key] = value
		result[index] = "{:" + key + "}"
	}
	return result
}
