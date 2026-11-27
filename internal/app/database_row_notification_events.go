package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database row change notifies, as in Notion, whoever follows the row page
// for its updates and whoever a person property newly names, unless that
// property's notify setting is none. The changes are read against the stored
// values before a mutation applies; those a person property names become
// personal notifications in the same commit, and the followers hear through
// the row's fan-out (database_row_notification_fanout.go), never the person
// who made the change.

const (
	databaseRowNotificationBatch        = 100
	databaseRowNotificationPreviewRunes = 200
	// A person's edits to one row within this window update one notification
	// for its followers instead of one for every saved keystroke.
	databaseRowNotificationWindowMillis = 10 * 60 * 1000
	databasePersonNotifyNone            = "none"
)

type plannedDatabaseRowNotification struct {
	RowID       string
	UserID      string
	Kind        string
	ActivityKey string
	// Preview and PreviewSpans are the person property's line, as Notion's
	// inbox says it: "Owner: @Ada" (notification_item_now.go).
	Preview      string
	PreviewSpans string
}

type databaseRowNotificationPlan struct {
	ActorID         string
	WorkspaceID     string
	ContainerPageID string
	MutationID      string
	Items           []plannedDatabaseRowNotification
	Fanouts         []plannedDatabaseRowFanout
	// Connection names the integration the change was written through, if
	// one (noticeConnection), which its notices name as Notion's inbox does.
	Connection string
}

// plannedDatabaseRowFanout is a changed row whose followers are to hear.
type plannedDatabaseRowFanout struct {
	RowID           string
	DataSourceID    string
	ActivityKey     string
	PropertyIDsJSON string
}

type databaseRowNotificationProperty struct {
	ID           string `db:"id"`
	DataSourceID string `db:"data_source_id"`
	Type         string `db:"type"`
	Name         string `db:"name"`
	Config       string `db:"config"`
}

type databaseRowNotificationValue struct {
	RowID      string `db:"row_id"`
	PropertyID string `db:"property_id"`
	Value      string `db:"value"`
}

func prepareDatabaseRowNotifications(
	app core.App,
	authorization pageAuthorization,
	actorID, mutationID string,
	operations []graphOperation,
) (databaseRowNotificationPlan, error) {
	plan := databaseRowNotificationPlan{
		ActorID: actorID, WorkspaceID: authorization.Workspace.WorkspaceID,
		ContainerPageID: authorization.Page.Id, MutationID: mutationID,
		Connection: noticeConnection(pageHistoryConnectionOf(app)),
	}
	rows := map[string]*graphDatabaseRow{}
	sourceIDs := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil && len(operation.Row.Values) > 0 {
			rows[operation.Row.ID] = operation.Row
			sourceIDs[operation.Row.DataSourceID] = struct{}{}
		}
	}
	if len(rows) == 0 {
		return plan, nil
	}
	rowIDs := make([]string, 0, len(rows))
	for rowID := range rows {
		rowIDs = append(rowIDs, rowID)
	}
	sort.Strings(rowIDs)
	properties, err := loadDatabaseRowNotificationProperties(app, sourceIDs)
	if err != nil {
		return plan, err
	}
	stored, existing, err := loadDatabaseRowNotificationValues(app, rowIDs)
	if err != nil {
		return plan, err
	}
	window := strconv.FormatInt(time.Now().UnixMilli()/databaseRowNotificationWindowMillis, 10)
	sources := make([]string, 0, len(sourceIDs))
	for sourceID := range sourceIDs {
		sources = append(sources, sourceID)
	}
	visibility := newDatabaseRowNotificationVisibility(app, sources)
	for _, rowID := range rowIDs {
		row := rows[rowID]
		sees := func(userID string, property databaseRowNotificationProperty) (bool, error) {
			return visibility.sees(userID, property, row, stored[rowID])
		}
		changed := []databaseRowNotificationProperty{}
		propertyIDs := make([]string, 0, len(row.Values))
		for propertyID := range row.Values {
			propertyIDs = append(propertyIDs, propertyID)
		}
		sort.Strings(propertyIDs)
		for _, propertyID := range propertyIDs {
			property, known := properties[propertyID]
			before := stored[rowID][propertyID]
			if !known || databaseRowNotificationSameValue(before, string(row.Values[propertyID].Value)) {
				continue
			}
			changed = append(changed, property)
			if property.Type != graphValueTypePerson || databasePersonNotifySetting(property.Config) == databasePersonNotifyNone {
				continue
			}
			line, lineSpans := "", ""
			for _, userID := range databaseRowNotificationAddedPeople(before, string(row.Values[propertyID].Value)) {
				// A connection's bot (notion_people_value_bots.go) has no inbox,
				// nor someone invited (people_value_invitees.go), whom the
				// invitation's email told.
				if userID == actorID || notionBotUserIDShape(userID) || peopleValueInviteeIDShape(userID) {
					continue
				}
				if shown, err := sees(userID, property); err != nil {
					return plan, err
				} else if !shown {
					continue
				}
				if line == "" {
					line, lineSpans = databasePersonLine(app, property.Name, peopleInValue(row.Values[propertyID].Value))
				}
				plan.Items = append(plan.Items, plannedDatabaseRowNotification{
					RowID: rowID, UserID: userID, Kind: "mention", Preview: line, PreviewSpans: lineSpans,
					ActivityKey: databasePersonActivityPrefix + rowID + ":" + propertyID + ":" + mutationID,
				})
			}
		}
		if _, found := existing[rowID]; !found || len(changed) == 0 {
			continue
		}
		// Property changes reach those following all updates, all of them,
		// through the row's fan-out; all comments is comments alone.
		changedIDs := make([]string, 0, len(changed))
		for _, property := range changed {
			changedIDs = append(changedIDs, property.ID)
		}
		encoded, err := json.Marshal(changedIDs)
		if err != nil {
			return plan, err
		}
		plan.Fanouts = append(plan.Fanouts, plannedDatabaseRowFanout{
			RowID: rowID, DataSourceID: row.DataSourceID, PropertyIDsJSON: string(encoded),
			ActivityKey: "database-row-update:" + rowID + ":" + actorID + ":" + window,
		})
	}
	return plan, nil
}

// loadDatabaseRowNotificationProperties reads the changed rows' properties.
func loadDatabaseRowNotificationProperties(
	app core.App, sourceIDs map[string]struct{},
) (map[string]databaseRowNotificationProperty, error) {
	ids := make([]string, 0, len(sourceIDs))
	for sourceID := range sourceIDs {
		ids = append(ids, sourceID)
	}
	sort.Strings(ids)
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	stored := []databaseRowNotificationProperty{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]], [[type]], [[name]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] IN (SELECT [[ids]].[[value]] FROM json_each({:sourceIds}) AS [[ids]])
	`).Bind(dbx.Params{"sourceIds": string(encoded)}).All(&stored); err != nil {
		return nil, err
	}
	properties := make(map[string]databaseRowNotificationProperty, len(stored))
	for _, property := range stored {
		properties[property.ID] = property
	}
	return properties, nil
}

func loadDatabaseRowNotificationValues(
	app core.App, rowIDs []string,
) (map[string]map[string]string, map[string]struct{}, error) {
	encoded, err := json.Marshal(rowIDs)
	if err != nil {
		return nil, nil, err
	}
	values := []databaseRowNotificationValue{}
	if err := app.DB().NewQuery(`
		SELECT [[row_id]], [[property_id]], [[value]] FROM {{leafen_database_row_values}}
		WHERE [[row_id]] IN (SELECT [[ids]].[[value]] FROM json_each({:rowIds}) AS [[ids]])
	`).Bind(dbx.Params{"rowIds": string(encoded)}).All(&values); err != nil {
		return nil, nil, err
	}
	stored := map[string]map[string]string{}
	for _, value := range values {
		if stored[value.RowID] == nil {
			stored[value.RowID] = map[string]string{}
		}
		stored[value.RowID][value.PropertyID] = value.Value
	}
	found := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_rows}} WHERE [[id]] IN (SELECT [[ids]].[[value]] FROM json_each({:rowIds}) AS [[ids]])
	`).Bind(dbx.Params{"rowIds": string(encoded)}).Column(&found); err != nil {
		return nil, nil, err
	}
	existing := make(map[string]struct{}, len(found))
	for _, rowID := range found {
		existing[rowID] = struct{}{}
	}
	return stored, existing, nil
}

// databaseRowNotificationSameValue compares two stored JSON values; a
// missing value is the same as an empty one.
func databaseRowNotificationSameValue(before, after string) bool {
	decode := func(raw string) any {
		var value any
		if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &value) != nil {
			return nil
		}
		switch typed := value.(type) {
		case string:
			if typed == "" {
				return nil
			}
		case []any:
			if len(typed) == 0 {
				return nil
			}
		}
		return value
	}
	return reflect.DeepEqual(decode(before), decode(after))
}

// databaseRowNotificationAddedPeople lists the people a person value names
// that the value before it did not.
func databaseRowNotificationAddedPeople(before, after string) []string {
	people := func(raw string) []string {
		ids := []string{}
		if json.Unmarshal([]byte(raw), &ids) != nil {
			return nil
		}
		return ids
	}
	previous := map[string]struct{}{}
	for _, id := range people(before) {
		previous[id] = struct{}{}
	}
	added := []string{}
	for _, id := range people(after) {
		if _, found := previous[id]; !found && strings.TrimSpace(id) != "" {
			previous[id] = struct{}{}
			added = append(added, id)
		}
	}
	return added
}

// databasePersonNotifySetting reads a person property's notify setting:
// users and groups, users (the default) or none. Leafen person values name
// people only, so the first two notify the same people.
func databasePersonNotifySetting(config string) string {
	settings := struct {
		Notify string `json:"notify"`
	}{}
	_ = json.Unmarshal([]byte(config), &settings)
	return settings.Notify
}

// deliverDatabaseRowNotifications writes the planned notifications, each to
// a person who can still open the row — through its database's page or its
// own share — each person's rows judged together (notificationItemsTold):
// a paste naming someone in a thousand rows judges the table's page for
// them once, not a thousand times while it holds the write.
func deliverDatabaseRowNotifications(
	ctx context.Context, app core.App, plan databaseRowNotificationPlan, now int64,
) error {
	if len(plan.Items) == 0 {
		return nil
	}
	people, targets := []string{}, map[string][]string{}
	for _, item := range plan.Items {
		if _, found := targets[item.UserID]; !found {
			people = append(people, item.UserID)
		}
		targets[item.UserID] = append(targets[item.UserID], databaseRowPageTarget(item.RowID))
	}
	// The rows' pages and records are read once for everyone; nothing is
	// written until all are judged.
	judge := withAccessMemo(app)
	told := make(map[string]map[string]bool, len(people))
	for _, userID := range people {
		allowed, err := notificationItemsTold(judge, userID, plan.ContainerPageID, targets[userID])
		if err != nil {
			return err
		}
		told[userID] = allowed
	}
	titles := map[string]string{}
	projections := make([]productNotificationProjection, 0, len(plan.Items))
	for _, item := range plan.Items {
		if !told[item.UserID][databaseRowPageTarget(item.RowID)] {
			continue
		}
		title, found := titles[item.RowID]
		if !found {
			// An untitled row stays so, named in each reader's language where shown.
			title = databaseRowPageTitle(app, item.RowID)
			titles[item.RowID] = title
		}
		preview := item.Preview
		if utf8.RuneCountInString(preview) > databaseRowNotificationPreviewRunes {
			preview = string([]rune(preview)[:databaseRowNotificationPreviewRunes])
		}
		id := sha256.Sum256([]byte(strings.Join([]string{
			"database-row-notification", plan.WorkspaceID, item.UserID, item.ActivityKey,
		}, "\x00")))
		event := sha256.Sum256([]byte(strings.Join([]string{
			"database-row-event", plan.WorkspaceID, item.RowID, plan.MutationID,
		}, "\x00")))
		projections = append(projections, productNotificationProjection{
			ID: hex.EncodeToString(id[:16]), EventID: hex.EncodeToString(event[:16]),
			WorkspaceID: plan.WorkspaceID, UserID: item.UserID, ActivityKey: item.ActivityKey,
			Kind: item.Kind, PageID: plan.ContainerPageID, BlockID: item.RowID,
			ActorID: plan.ActorID, Title: title, Preview: preview, PreviewSpans: item.PreviewSpans,
			Target:     databaseRowPageTarget(item.RowID),
			OccurredAt: now, Personal: true, ActorConnection: plan.Connection,
		})
	}
	for start := 0; start < len(projections); start += databaseRowNotificationBatch {
		end := min(start+databaseRowNotificationBatch, len(projections))
		if _, err := upsertProductNotifications(ctx, app, projections[start:end], now); err != nil {
			return err
		}
	}
	return nil
}

// databaseRowNotificationVisibility reads, per recipient, whether a changed
// property shows them in the row under its access rule
// (database_property_access.go): one that doesn't goes unnamed. An unlinked
// item they have no role in shows them nothing (database_row_unlinked_access.go).
type databaseRowNotificationVisibility struct {
	app     core.App
	sources []string
	readers map[string]*databasePropertyAccessReader
	items   map[string]*databaseRowAccess
	loaded  map[string]bool
}

func newDatabaseRowNotificationVisibility(app core.App, sources []string) *databaseRowNotificationVisibility {
	return &databaseRowNotificationVisibility{app: app, sources: sources,
		readers: map[string]*databasePropertyAccessReader{}, items: map[string]*databaseRowAccess{}, loaded: map[string]bool{}}
}

func (visibility *databaseRowNotificationVisibility) sees(userID string, property databaseRowNotificationProperty,
	row *graphDatabaseRow, before map[string]string) (bool, error) {
	if !visibility.loaded[userID] {
		reader, err := newDatabasePropertyAccessReader(visibility.app, userID, visibility.sources)
		if err != nil {
			return false, err
		}
		items, err := loadDatabaseRowAccess(visibility.app, userID, visibility.sources)
		if err != nil {
			return false, err
		}
		visibility.readers[userID], visibility.items[userID], visibility.loaded[userID] = reader, items, true
	}
	if visibility.items[userID].hides(row.ID) {
		return false, nil
	}
	reader := visibility.readers[userID]
	if !reader.restricted(property.DataSourceID, property.ID) {
		return true, nil
	}
	level, err := reader.level(property.DataSourceID, property.ID, &databasePropertyAccessRow{ID: row.ID, People: func(propertyID string) []string {
		if value, changed := row.Values[propertyID]; changed {
			return peopleInValue(value.Value)
		}
		return peopleInValue([]byte(before[propertyID]))
	}})
	return level != propertyAccessNone, err
}
