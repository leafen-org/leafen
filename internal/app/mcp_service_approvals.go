package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// An agent's call to a service's tool that asks first (by its person's
// choice, or one that may destroy something) waits for the person's yes,
// asked inside their conversation with the agent (person_ask.go). The
// request is bound to the connection, the service, the tool, the person and
// that input, used once, and declined by itself after a day; allowed "for an
// hour", the tool runs on that connection meanwhile without asking, and
// "always" every tool of the service runs without asking from then on
// (mcp_service_tool_choices.go). A request with no service is Leafen's own
// action its token asks before, its route as the tool
// (mcp_action_approvals.go): allowed once or for an hour, never always — its
// token's page turns the asking off.

const (
	mcpServiceApprovalSeconds = 24 * 60 * 60
	mcpServiceApprovalHour    = time.Hour
)

func mcpServiceInputHash(serviceID, tool string, arguments map[string]any) (string, string) {
	if arguments == nil {
		arguments = map[string]any{}
	}
	canonical, _ := json.Marshal(arguments)
	digest := sha256.Sum256([]byte(serviceID + "\x00" + tool + "\x00" + string(canonical)))
	return hex.EncodeToString(digest[:]), string(canonical)
}

// mcpServiceAllowedForHour says the person allowed the tool on this
// connection for an hour that hasn't ended.
func mcpServiceAllowedForHour(app core.App, authority mcpAuthority, serviceID, tool string) bool {
	found := 0
	_ = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_mcp_service_approvals}} WHERE [[service_id]] = {:service} AND [[tool]] = {:tool}
		AND [[connection_id]] = {:connection} AND [[person_id]] = {:person} AND [[allow_until]] > {:now}`).Bind(dbx.Params{
		"service": serviceID, "tool": tool, "connection": authority.ConnectionID, "person": authority.owner(), "now": time.Now().UnixMilli(),
	}).Row(&found)
	return found > 0
}

// mcpApprovalRequests are one call's requests to its person — a service's
// tool, or Leafen's own action with no service — as the asking boundary
// keeps them (personAskRequests).
type mcpApprovalRequests struct {
	app       core.App
	authority mcpAuthority
	serviceID string
	tool      string
	inputHash string
	preview   string
}

func (requests mcpApprovalRequests) params(extra dbx.Params) dbx.Params {
	params := dbx.Params{"person": requests.authority.owner(), "service": requests.serviceID, "tool": requests.tool,
		"connection": requests.authority.ConnectionID, "input": requests.inputHash, "now": time.Now().UnixMilli()}
	for key, value := range extra {
		params[key] = value
	}
	return params
}

const mcpApprovalRequestMatch = `[[person_id]] = {:person} AND [[service_id]] = {:service} AND [[tool]] = {:tool}
	AND [[connection_id]] = {:connection} AND [[input_hash]] = {:input}`

func (requests mcpApprovalRequests) waiting() (string, int64, error) {
	found := struct {
		ID        string `db:"id"`
		ExpiresAt int64  `db:"expires_at"`
	}{}
	err := requests.app.DB().NewQuery(`SELECT [[id]], [[expires_at]] FROM {{leafen_mcp_service_approvals}}
		WHERE ` + mcpApprovalRequestMatch + ` AND [[status]] = 'pending' AND [[expires_at]] > {:now}
		ORDER BY [[created]] DESC LIMIT 1`).Bind(requests.params(nil)).One(&found)
	if err == nil {
		return found.ID, found.ExpiresAt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", 0, err
	}
	now := time.Now()
	preview := requests.preview
	if runes := []rune(preview); len(runes) > 600 {
		preview = string(runes[:600]) + "…"
	}
	id, expiresAt := security.RandomString(24), now.Add(mcpServiceApprovalSeconds*time.Second).UnixMilli()
	_, err = requests.app.DB().Insert("leafen_mcp_service_approvals", dbx.Params{
		"id": id, "service_id": requests.serviceID, "person_id": requests.authority.owner(), "connection_id": requests.authority.ConnectionID,
		"tool": requests.tool, "input_hash": requests.inputHash, "preview": preview, "status": "pending", "created": now.UnixMilli(),
		"expires_at": expiresAt,
	}).Execute()
	return id, expiresAt, err
}

func (requests mcpApprovalRequests) status(id string) string {
	found := struct {
		Status    string `db:"status"`
		ExpiresAt int64  `db:"expires_at"`
	}{}
	if err := requests.app.DB().NewQuery(`SELECT [[status]], [[expires_at]] FROM {{leafen_mcp_service_approvals}}
		WHERE [[id]] = {:id} AND ` + mcpApprovalRequestMatch).Bind(requests.params(dbx.Params{"id": id})).One(&found); err != nil {
		return ""
	}
	// A yes not spent within the day runs out with the request.
	if found.Status != "used" && found.Status != "declined" && found.ExpiresAt <= time.Now().UnixMilli() {
		return ""
	}
	return found.Status
}

func (requests mcpApprovalRequests) decide(id string, yes, forHour, always bool) error {
	status, allowUntil := "declined", int64(0)
	if yes {
		status = "approved"
		if forHour {
			allowUntil = time.Now().Add(mcpServiceApprovalHour).UnixMilli()
		}
	}
	result, err := requests.app.DB().NewQuery(`UPDATE {{leafen_mcp_service_approvals}} SET [[status]] = {:status}, [[decided_at]] = {:now},
		[[allow_until]] = {:allowUntil} WHERE [[id]] = {:id} AND [[status]] = 'pending' AND [[expires_at]] > {:now} AND ` + mcpApprovalRequestMatch).
		Bind(requests.params(dbx.Params{"status": status, "allowUntil": allowUntil, "id": id})).Execute()
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("%w: the request ran out; call without it to ask again", errMCPToolInvalid)
	}
	// Notion's "항상 허용": the service's tools run without asking from now
	// on; Leafen's own actions ask again until the token's page turns that off.
	if always && requests.serviceID != "" {
		return setMCPServiceAlwaysAllowed(requests.app, requests.authority.owner(), requests.serviceID, true)
	}
	return nil
}

func (requests mcpApprovalRequests) spend(id string) error {
	result, err := requests.app.DB().NewQuery(`UPDATE {{leafen_mcp_service_approvals}} SET [[status]] = 'used', [[used_at]] = {:now}
		WHERE [[id]] = {:id} AND [[status]] = 'approved' AND ` + mcpApprovalRequestMatch).Bind(requests.params(dbx.Params{"id": id})).Execute()
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("%w: this yes was used; call without it to ask again", errMCPToolInvalid)
	}
	return nil
}
