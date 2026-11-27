package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/reearth/ygo/crdt"
)

// Notion's ID property rules (seen 2026-11-11 through its API and app): a
// data source has one ID property, one in Deleted properties counting, and
// a prefix is one data source's in its workspace — an ID property in Deleted
// properties keeps holding its prefix, a data source in the trash loses it
// once another takes it.
// A person's change, in the app, the public API or MCP, keeps them
// (guardDatabaseUniqueIDProperties); the app's menus and prefix editor ask
// first (serveDatabaseUniqueIDState) and the public API also refuses
// turning a property into an ID or an ID into another type
// (notion_unique_id_prefix.go). Copies, imports and the system's rewrites
// keep what they copy.

const (
	uniqueIDSingleMessage  = "A data source has at most one unique ID property."
	uniqueIDUnsavedMessage = "Unsaved transactions: More than one ID property per database is currently not supported."
	uniqueIDManyMessage    = "More than one ID property per database is currently not supported."
)

// uniqueIDPrefixInUse is the answer to a prefix another data source of the
// workspace has, as Notion's API refuses it, in Leafen's words.
func uniqueIDPrefixInUse(prefix string) error {
	return graphConflictError(errorCodeGraphChangeInvalid, fmt.Sprintf(
		"Unique ID prefix %q is taken: in a workspace, each prefix belongs to one data source.",
		strings.ToUpper(prefix),
	))
}

// uniqueIDConfigRawPrefix is the prefix a property's settings keep, as kept.
func uniqueIDConfigRawPrefix(config json.RawMessage) string {
	settings := struct {
		IDPrefix any `json:"idPrefix"`
	}{}
	_ = json.Unmarshal(config, &settings)
	prefix, _ := settings.IDPrefix.(string)
	return prefix
}

// uniqueIDConfigPrefix is the prefix a property's settings keep, in capitals.
func uniqueIDConfigPrefix(config json.RawMessage) string {
	settings := struct {
		IDPrefix any `json:"idPrefix"`
	}{}
	if json.Unmarshal(config, &settings) != nil {
		return ""
	}
	prefix, _ := settings.IDPrefix.(string)
	return strings.ToUpper(strings.TrimSpace(prefix))
}

// uniqueIDPrefixHolder is an ID property of another data source with the
// prefix asked about.
type uniqueIDPrefixHolder struct {
	ID     string `db:"id"`
	Owner  string `db:"owner"`
	Source string `db:"data_source_id"`
	PageID string `db:"page_id"`
	Config string `db:"config"`
}

// uniqueIDPrefixTaken reports whether another data source of the workspace,
// out of the trash, has an ID property with the prefix.
func uniqueIDPrefixTaken(app core.App, workspaceID, dataSourceID, prefix string) (bool, error) {
	taken, _, err := uniqueIDPrefixHolders(app, workspaceID, dataSourceID, prefix)
	return taken, err
}

// uniqueIDPrefixHolders reports whether another data source of the
// workspace, out of the trash, has an ID property with the prefix, and the
// ID properties of those in the trash that have it.
func uniqueIDPrefixHolders(
	app core.App, workspaceID, dataSourceID, prefix string,
) (bool, []uniqueIDPrefixHolder, error) {
	prefix = strings.ToUpper(strings.TrimSpace(prefix))
	if prefix == "" {
		return false, nil, nil
	}
	holders := []uniqueIDPrefixHolder{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[data_source_id]], [[page_id]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[type]] = {:type} AND [[data_source_id]] <> {:sourceId}
			AND upper(trim(COALESCE(json_extract([[config]], '$.idPrefix'), ''))) = {:prefix}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "type": graphValueTypeUniqueID, "sourceId": dataSourceID, "prefix": prefix,
	}).All(&holders); err != nil {
		return false, nil, err
	}
	trashed := []uniqueIDPrefixHolder{}
	for _, holder := range holders {
		inTrash, err := pageInTrashPath(app, holder.PageID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, nil, err
		}
		inPlace := false
		if err := app.DB().NewQuery(`
			SELECT EXISTS (SELECT 1 FROM {{leafen_data_source_trash_in_place}} WHERE [[data_source_id]] = {:sourceId})
		`).Bind(dbx.Params{"sourceId": holder.Source}).Row(&inPlace); err != nil {
			return false, nil, err
		}
		if !inTrash && !inPlace {
			return true, nil, nil
		}
		trashed = append(trashed, holder)
	}
	return false, trashed, nil
}

// takeUniqueIDPrefixFromTrash leaves the ID properties of data sources in
// the trash without the prefix another one now takes, as Notion's: its rows'
// IDs lose it, and it stays without one once restored (seen 2026-11-11). The
// trash alone keeps it.
func takeUniqueIDPrefixFromTrash(app core.App, holders []uniqueIDPrefixHolder) error {
	for _, holder := range holders {
		config := map[string]any{}
		if json.Unmarshal([]byte(holder.Config), &config) != nil {
			config = map[string]any{}
		}
		delete(config, "idPrefix")
		encoded, err := json.Marshal(config)
		if err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_database_properties}} SET [[config]] = {:config}, [[updated]] = {:now} WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": holder.ID, "config": string(encoded), "now": time.Now().UnixMilli()}).Execute(); err != nil {
			return err
		}
		// Its page records the change, so a screen that kept it reads it again.
		if err := recordServerGraphChange(app, holder.Owner, holder.PageID,
			"unique-id-prefix-taken:"+holder.ID+":"+security.RandomString(12),
			crdt.EncodeStateAsUpdateV1(crdt.New(), nil), graphMutationCounts{}); err != nil {
			return err
		}
	}
	return nil
}

// databaseSourceUniqueIDProperties counts a data source's ID properties
// other than those excepted: in use, and in Deleted properties — where
// those removed count too.
func databaseSourceUniqueIDProperties(
	app core.App, dataSourceID string, except, removed map[string]struct{},
) (live, deleted int, err error) {
	rows := []struct {
		ID      string `db:"id"`
		Deleted bool   `db:"deleted"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[property]].[[id]],
			EXISTS (SELECT 1 FROM {{leafen_database_property_trash}} AS [[trash]]
				WHERE [[trash]].[[property_id]] = [[property]].[[id]]) AS [[deleted]]
		FROM {{leafen_database_properties}} AS [[property]]
		WHERE [[property]].[[data_source_id]] = {:sourceId} AND [[property]].[[type]] = {:type}
	`).Bind(dbx.Params{"sourceId": dataSourceID, "type": graphValueTypeUniqueID}).All(&rows); err != nil {
		return 0, 0, err
	}
	for _, row := range rows {
		if _, skip := except[row.ID]; skip {
			continue
		}
		_, gone := removed[row.ID]
		switch {
		case row.Deleted || gone:
			deleted++
		default:
			live++
		}
	}
	return live, deleted, nil
}

// guardDatabaseSchemaOperations keeps what a person's change does to a
// database's properties within the rules its features and Notion's ID
// properties set.
func guardDatabaseSchemaOperations(app core.App, workspaceID, pageID string, operations []graphOperation) error {
	if err := guardLockedDatabaseRollups(app, pageID, operations); err != nil {
		return err
	}
	return guardDatabaseUniqueIDProperties(app, workspaceID, operations)
}

// guardDatabaseUniqueIDProperties refuses a person's change that gives a
// data source a second ID property, or an ID property a prefix Notion
// refuses (notion_unique_id_prefix.go) or another data source of the
// workspace has; the prefix is asked about first, as Notion's API answers.
// A prefix one in the trash has is taken from it.
func guardDatabaseUniqueIDProperties(app core.App, workspaceID string, operations []graphOperation) error {
	removed := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindDeleteProperty && operation.PropertyDeletion != nil {
			removed[operation.PropertyDeletion.ID] = struct{}{}
		}
	}
	added := map[string][]string{}
	for _, operation := range operations {
		property := operation.Property
		if operation.Kind != graphOperationKindUpsertProperty || property == nil || property.Type != graphValueTypeUniqueID {
			continue
		}
		stored := struct {
			Type   string `db:"type"`
			Config string `db:"config"`
		}{}
		err := app.DB().NewQuery(`
			SELECT [[type]], [[config]] FROM {{leafen_database_properties}} WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": property.ID}).One(&stored)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		wasID := err == nil && stored.Type == graphValueTypeUniqueID
		// One it already had stays, as copies keep what they copy.
		if raw := uniqueIDConfigRawPrefix(property.Config); raw != "" &&
			(!wasID || raw != uniqueIDConfigRawPrefix(json.RawMessage(stored.Config))) {
			if _, valid := uniqueIDPrefix(raw); !valid {
				return graphInputError(errorCodeGraphChangeInvalid, fmt.Sprintf(uniqueIDPrefixRefusedMessage, raw))
			}
		}
		prefix := uniqueIDConfigPrefix(property.Config)
		if prefix != "" && (!wasID || uniqueIDConfigPrefix(json.RawMessage(stored.Config)) != prefix) {
			taken, trashed, err := uniqueIDPrefixHolders(app, workspaceID, property.DataSourceID, prefix)
			if err != nil {
				return err
			}
			if taken {
				return uniqueIDPrefixInUse(prefix)
			}
			if err := takeUniqueIDPrefixFromTrash(app, trashed); err != nil {
				return err
			}
		}
		if !wasID {
			added[property.DataSourceID] = append(added[property.DataSourceID], property.ID)
		}
	}
	for dataSourceID, made := range added {
		except := map[string]struct{}{}
		for _, id := range made {
			except[id] = struct{}{}
		}
		// One removed in the same change is in Deleted properties after it.
		live, deleted, err := databaseSourceUniqueIDProperties(app, dataSourceID, except, removed)
		if err != nil {
			return err
		}
		switch {
		case live > 0:
			return graphInputError(errorCodeGraphChangeInvalid, uniqueIDSingleMessage)
		case deleted > 0:
			return graphInputError(errorCodeGraphChangeInvalid, uniqueIDUnsavedMessage)
		case len(made) > 1:
			return graphInputError(errorCodeGraphChangeInvalid, uniqueIDManyMessage)
		}
	}
	return nil
}

// serveDatabaseUniqueIDState tells the app's menus and prefix editor what a
// data source's ID property may be: whether it has an ID property other
// than the one named (in use or in Deleted properties), and whether the
// prefix asked about is another data source's in the workspace.
func serveDatabaseUniqueIDState(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	dataSourceID := strings.TrimSpace(event.Request.PathValue("dataSourceId"))
	// The data source is read where it is kept: its own page.
	pageID := ""
	if err := event.App.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_database_properties}} WHERE [[data_source_id]] = {:sourceId} LIMIT 1
	`).Bind(dbx.Params{"sourceId": dataSourceID}).Row(&pageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apiError(event, http.StatusNotFound, errorCodeDatabaseDataSourceNotFound, "Data source not found.")
		}
		return writeDatabasePropertyTrashError(event, err)
	}
	authorization, err := authorizePageBody(event.App, user.Id, pageID, permissionWorkspaceRead)
	if err != nil {
		return writeDatabasePropertyTrashError(event, err)
	}
	query := event.Request.URL.Query()
	property := strings.TrimSpace(query.Get("property"))
	prefix := strings.TrimSpace(query.Get("prefix"))
	if len(prefix) > 64 || len(property) > 64 {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseDataSourceInvalid, "The prefix or property is too long.")
	}
	live, deleted, err := databaseSourceUniqueIDProperties(event.App, dataSourceID, map[string]struct{}{property: {}}, nil)
	if err != nil {
		return writeDatabasePropertyTrashError(event, err)
	}
	taken, err := uniqueIDPrefixTaken(event.App, authorization.Page.GetString("workspace"), dataSourceID, prefix)
	if err != nil {
		return writeDatabasePropertyTrashError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, databaseUniqueIDStateResponse{OtherUniqueID: live+deleted > 0, PrefixTaken: taken})
}
