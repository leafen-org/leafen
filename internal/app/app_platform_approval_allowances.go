//go:build !leafen_noapps

package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What an approval shows and how long it may last (docs/app-platform.md 5):
// the input as lines a person reads — each field by the name the action's
// input schema gives it, a person by their name, a yes or no — and, beside
// "just this once", allowing the action for an hour on the same way in (the
// app's screen, or the agent's connection), so it doesn't ask again
// meanwhile. An organization the administrator oversees asks for every
// action, and allows nothing for an hour on the app's screen; an agent's
// connection is allowed an hour there too (decided 2026-11-13).

const (
	appApprovalAllowanceDuration = time.Hour
	appApprovalChannelScreen     = "screen"
	appApprovalFieldsMax         = 20
	appApprovalValueRunes        = 500
)

// appApprovalFields reads an action's input for the person deciding.
func appApprovalFields(app core.App, installID string, action appManifestAction, input []byte, tenancy, personID string) []appApprovalFieldWire {
	values := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	if decoder.Decode(&values) != nil {
		return []appApprovalFieldWire{}
	}
	schema := struct {
		Properties map[string]struct {
			Title string `json:"title"`
		} `json:"properties"`
	}{}
	_ = json.Unmarshal(action.Input, &schema)
	order := appSchemaPropertyOrder(action.Input)
	for key := range values {
		if !appOneOf(key, order) {
			order = append(order, key)
		}
	}
	fields := []appApprovalFieldWire{}
	// A page reads as its title (app_platform_approval_page_title.go).
	appWorkspaceID := ""
	_ = app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_app_workspaces}} WHERE [[install_id]] = {:installId} AND [[tenancy_key]] = {:tenancy}`).
		Bind(dbx.Params{"installId": installID, "tenancy": tenancy}).Row(&appWorkspaceID)
	for _, key := range order {
		value, present := values[key]
		if !present {
			continue
		}
		label := key
		if title := strings.TrimSpace(schema.Properties[key].Title); title != "" {
			label = title
		}
		field := appApprovalFieldValue(app, value, tenancy, appWorkspaceID, personID)
		field.Label, field.Labels = label, appTextOrEmpty(action.InputLabels[key])
		if fields = append(fields, field); len(fields) == appApprovalFieldsMax {
			break
		}
	}
	return fields
}

func appApprovalFieldValue(app core.App, value any, tenancy, appWorkspaceID, personID string) appApprovalFieldWire {
	switch typed := value.(type) {
	case nil:
		return appApprovalFieldWire{Kind: "empty"}
	case bool:
		return appApprovalFieldWire{Kind: "boolean", Value: strconv.FormatBool(typed)}
	case json.Number:
		return appApprovalFieldWire{Kind: "number", Value: typed.String()}
	case string:
		if name, person := appApprovalPersonName(app, typed, tenancy); person {
			return appApprovalFieldWire{Kind: "person", Value: name}
		}
		if title, page := appApprovalPageTitle(app, typed, appWorkspaceID, personID); page {
			return appApprovalFieldWire{Kind: "page", Value: appApprovalShort(title)}
		}
		if strings.TrimSpace(typed) == "" {
			return appApprovalFieldWire{Kind: "empty"}
		}
		return appApprovalFieldWire{Kind: "text", Value: appApprovalShort(typed)}
	case []any:
		parts := []string{}
		for _, item := range typed {
			part := appApprovalFieldValue(app, item, tenancy, appWorkspaceID, personID)
			if part.Kind == "data" || part.Kind == "list" {
				return appApprovalFieldWire{Kind: "data", Value: appApprovalJSON(typed)}
			}
			parts = append(parts, part.Value)
		}
		return appApprovalFieldWire{Kind: "list", Value: appApprovalShort(strings.Join(parts, ", "))}
	}
	return appApprovalFieldWire{Kind: "data", Value: appApprovalJSON(value)}
}

func appApprovalShort(text string) string {
	if runes := []rune(text); len(runes) > appApprovalValueRunes {
		return string(runes[:appApprovalValueRunes]) + "…"
	}
	return text
}

func appApprovalJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return appApprovalShort(string(encoded))
}

// appApprovalPersonName names a person the input refers to by ID, when
// they are among the organization's people.
func appApprovalPersonName(app core.App, id, tenancy string) (string, bool) {
	if len(id) != 15 || strings.ContainsAny(id, " \n") {
		return "", false
	}
	var name, email string
	if err := app.DB().NewQuery(`SELECT [[name]], [[email]] FROM {{users}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": id}).Row(&name, &email); err != nil {
		return "", false
	}
	if workspaces, err := appPersonOrdinaryWorkspaces(app, id, tenancy); err != nil || len(workspaces) == 0 {
		return "", false
	}
	if strings.TrimSpace(name) == "" {
		name = email
	}
	return name, true
}

// appApprovalHourAllowed says a person may allow an app's action for an
// hour on the way in it was asked on: on the app's screen unless the
// administrator oversees the organization, and always on an agent's
// connection.
func appApprovalHourAllowed(app core.App, tenancy, channel string) bool {
	return channel != appApprovalChannelScreen || !appOverseen(app, tenancy)
}

// appApprovalRequired says whether running the action asks the person:
// as the manifest says, or every action under oversight — unless they
// allowed it for the hour on this way in, where an hour is allowed.
func appApprovalRequired(app core.App, install storedAppInstall, action appManifestAction, personID, channel, tenancy string) (bool, error) {
	if !action.RequiresApproval && !appOverseen(app, tenancy) {
		return false, nil
	}
	if !appApprovalHourAllowed(app, tenancy, channel) {
		return true, nil
	}
	var expires int64
	err := app.DB().NewQuery(`SELECT [[expires_at]] FROM {{leafen_app_approval_allowances}}
		WHERE [[install_id]] = {:install} AND [[action]] = {:action} AND [[person_id]] = {:person} AND [[channel]] = {:channel}`).
		Bind(dbx.Params{"install": install.ID, "action": action.Key, "person": personID, "channel": channel}).Row(&expires)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	return expires <= time.Now().UnixMilli(), nil
}

// allowAppActionForHour keeps the person's "for an hour" for the action on
// the way in it was asked on, and marks the approval it was given with.
func allowAppActionForHour(app core.App, install storedAppInstall, approvalID, action, personID, channel, tenancy string) error {
	if !appApprovalHourAllowed(app, tenancy, channel) {
		return nil
	}
	now := time.Now()
	return app.RunInTransaction(func(tx core.App) error {
		if _, err := tx.DB().NewQuery(`INSERT INTO {{leafen_app_approval_allowances}}
			([[install_id]], [[action]], [[person_id]], [[channel]], [[tenancy]], [[created]], [[expires_at]])
			VALUES ({:install}, {:action}, {:person}, {:channel}, {:tenancy}, {:now}, {:expires})
			ON CONFLICT ([[install_id]], [[action]], [[person_id]], [[channel]]) DO UPDATE SET [[created]] = excluded.[[created]], [[expires_at]] = excluded.[[expires_at]]`).
			Bind(dbx.Params{"install": install.ID, "action": action, "person": personID, "channel": channel, "tenancy": tenancy,
				"now": now.UnixMilli(), "expires": now.Add(appApprovalAllowanceDuration).UnixMilli()}).Execute(); err != nil {
			return err
		}
		if _, err := tx.DB().NewQuery(`UPDATE {{leafen_action_approvals}} SET [[for_hour]] = 1 WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": approvalID}).Execute(); err != nil {
			return err
		}
		return recordAppAudit(tx, install, "approval-allowance", personID, map[string]any{"action": action, "channel": channel, "minutes": 60})
	})
}

// appApprovalDecision reads what the person chose: just once, or the hour.
func appApprovalDecision(event *core.RequestEvent) bool {
	decision := appApprovalDecisionInput{}
	_ = json.NewDecoder(event.Request.Body).Decode(&decision)
	return decision.For == "hour"
}

// setAppOversightRoute turns the administrator's oversight of an
// organization's apps on or off; turning it on ends every hour allowed there.
func setAppOversightRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	user, _ := authenticatedUser(event)
	input := appOversightInput{}
	if err := json.NewDecoder(event.Request.Body).Decode(&input); err != nil {
		return appPlatformFailure(event, bodyRefusal(errAppInputInvalid, "oversight choice", err))
	}
	tenancies, err := appTenancies(event.App)
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if refused := appTenancyRefusal("tenancyKey", input.TenancyKey, tenancies); refused != nil {
		return appPlatformFailure(event, refused)
	}
	// A key's call asks its person once the setting reads (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	err = event.App.RunInTransaction(func(tx core.App) error {
		action := "app_oversight.disabled"
		if input.Overseen {
			action = "app_oversight.enabled"
			if _, err := tx.DB().NewQuery(`INSERT INTO {{leafen_app_oversight}} ([[tenancy_key]], [[updated_by]], [[updated_at]])
				VALUES ({:tenancy}, {:user}, {:now}) ON CONFLICT ([[tenancy_key]]) DO UPDATE SET [[updated_by]] = excluded.[[updated_by]], [[updated_at]] = excluded.[[updated_at]]`).
				Bind(dbx.Params{"tenancy": input.TenancyKey, "user": user.Id, "now": time.Now().UnixMilli()}).Execute(); err != nil {
				return err
			}
			if _, err := tx.DB().NewQuery(`DELETE FROM {{leafen_app_approval_allowances}} WHERE [[tenancy]] = {:tenancy}`).
				Bind(dbx.Params{"tenancy": input.TenancyKey}).Execute(); err != nil {
				return err
			}
		} else if _, err := tx.DB().NewQuery(`DELETE FROM {{leafen_app_oversight}} WHERE [[tenancy_key]] = {:tenancy}`).
			Bind(dbx.Params{"tenancy": input.TenancyKey}).Execute(); err != nil {
			return err
		}
		if organizationID := appTenancyOrganization(input.TenancyKey); organizationID != "" {
			return recordOrganizationAudit(tx, organizationID, user.Id, action, "app_oversight", organizationID)
		}
		return nil
	})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if tenancies, err = appTenancies(event.App); err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, tenancies)
}

// appApprovalActionName is an app's action's name in each language, or none
// for a key it doesn't declare.
func appApprovalActionName(manifest appManifest, key string) map[string]string {
	if action, found := appManifestActionByKey(manifest, key); found {
		return appTextOrEmpty(action.Name)
	}
	return map[string]string{}
}

// loadAppApprovals is what the administrator sees of an app's approvals, a
// window at a time, newest first, from after on: who, what, on which way in,
// what they decided and whether for an hour.
func loadAppApprovals(app core.App, install storedAppInstall, manifest appManifest, after timeIDCursor) ([]appApprovalHistoryWire, string, error) {
	type approvalRow struct {
		ID         string `db:"id"`
		Action     string `db:"action"`
		Person     string `db:"person"`
		Connection string `db:"connection"`
		Status     string `db:"status"`
		ForHour    bool   `db:"for_hour"`
		Created    int64  `db:"created"`
		Expires    int64  `db:"expires_at"`
		Decided    int64  `db:"decided_at"`
	}
	rows := []approvalRow{}
	if err := app.DB().NewQuery(`SELECT approval.[[id]], approval.[[action]], COALESCE(NULLIF(person.[[name]], ''), person.[[email]], '') AS [[person]],
			COALESCE(connection.[[client_name]], '') AS [[connection]], approval.[[status]], approval.[[for_hour]], approval.[[created]],
			approval.[[expires_at]], approval.[[decided_at]]
		FROM {{leafen_action_approvals}} approval
		LEFT JOIN {{users}} person ON person.[[id]] = approval.[[person_id]]
		LEFT JOIN {{leafen_mcp_connections}} connection ON approval.[[connection_id]] != '' AND connection.[[id]] = approval.[[connection_id]]
		WHERE approval.[[install_id]] = {:install}
			AND ({:afterId} = '' OR approval.[[created]] < {:afterAt} OR (approval.[[created]] = {:afterAt} AND approval.[[id]] > {:afterId}))
		ORDER BY approval.[[created]] DESC, approval.[[id]] LIMIT {:limit}`).
		Bind(dbx.Params{"install": install.ID, "afterAt": after.At, "afterId": after.ID, "limit": appApprovalsPerWindow + 1}).All(&rows); err != nil {
		return nil, "", err
	}
	rows, next := appWindowOf(rows, appApprovalsPerWindow, func(row approvalRow) (int64, string) { return row.Created, row.ID })
	now := time.Now().UnixMilli()
	approvals := make([]appApprovalHistoryWire, 0, len(rows))
	for _, row := range rows {
		if row.Status == "pending" && row.Expires <= now {
			row.Status = "expired"
		}
		approvals = append(approvals, appApprovalHistoryWire{Action: row.Action, ActionName: appApprovalActionName(manifest, row.Action), Person: row.Person,
			Way: row.Connection, Status: row.Status, ForHour: row.ForHour, Created: row.Created, DecidedAt: row.Decided})
	}
	return approvals, next, nil
}

// loadAppAllowances are the hours of an app's actions allowed now.
func loadAppAllowances(app core.App, install storedAppInstall, manifest appManifest) ([]appApprovalAllowanceWire, error) {
	allowed := []struct {
		Action     string `db:"action"`
		Person     string `db:"person"`
		Connection string `db:"connection"`
		Expires    int64  `db:"expires_at"`
	}{}
	if err := app.DB().NewQuery(`SELECT allowance.[[action]], COALESCE(NULLIF(person.[[name]], ''), person.[[email]], '') AS [[person]],
			COALESCE(connection.[[client_name]], '') AS [[connection]], allowance.[[expires_at]]
		FROM {{leafen_app_approval_allowances}} allowance
		LEFT JOIN {{users}} person ON person.[[id]] = allowance.[[person_id]]
		LEFT JOIN {{leafen_mcp_connections}} connection ON connection.[[id]] = allowance.[[channel]]
		WHERE allowance.[[install_id]] = {:install} AND allowance.[[expires_at]] > {:now} ORDER BY allowance.[[expires_at]] DESC`).
		Bind(dbx.Params{"install": install.ID, "now": time.Now().UnixMilli()}).All(&allowed); err != nil {
		return nil, err
	}
	allowances := []appApprovalAllowanceWire{}
	for _, row := range allowed {
		allowances = append(allowances, appApprovalAllowanceWire{Action: row.Action, ActionName: appApprovalActionName(manifest, row.Action), Person: row.Person,
			Way: row.Connection, ExpiresAt: row.Expires})
	}
	return allowances, nil
}

func pruneAppApprovalAllowances(app core.App, now time.Time) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_app_approval_allowances}} WHERE [[expires_at]] < {:now}`).
		Bind(dbx.Params{"now": now.UnixMilli()}).Execute()
	return err
}
