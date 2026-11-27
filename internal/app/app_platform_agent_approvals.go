//go:build !leafen_noapps

package app

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Agents' approvals (docs/app-platform.md 5·13, T11·T13): an agent's call to
// an app's tool that needs approval waits for the person, asked inside their
// conversation with the agent (app_platform_agent_ask.go) — never through
// the app, a key's own call or a Leafen page. The request is bound to the
// connection, the tool, the person and that input, used once, and declined
// by itself after a day without an answer.

// appAgentPending is a request waiting for the person, as the agent hears
// of it: its id, when it runs out, the question in the person's words and
// what the agent does next.
type appAgentPending struct {
	ApprovalID string    `json:"approvalId"`
	ExpiresAt  int64     `json:"expiresAt"`
	Ask        personAsk `json:"ask"`
	Message    string    `json:"message"`
}

type storedAgentApproval struct {
	ID           string `db:"id"`
	InstallID    string `db:"install_id"`
	Action       string `db:"action"`
	PersonID     string `db:"person_id"`
	ConnectionID string `db:"connection_id"`
	Tenancy      string `db:"tenancy"`
	Preview      string `db:"preview"`
	Fields       string `db:"fields"`
	Status       string `db:"status"`
	Created      int64  `db:"created"`
	ExpiresAt    int64  `db:"expires_at"`
	DecidedAt    int64  `db:"decided_at"`
}

func requestAgentApproval(app core.App, usable usableApp, action appManifestAction, authority mcpAuthority, inputHash string,
	input []byte) (appAgentPending, error) {
	now := time.Now()
	preview := string(input)
	if runes := []rune(preview); len(runes) > 600 {
		preview = string(runes[:600]) + "…"
	}
	pending := appAgentPending{ApprovalID: security.RandomString(24), ExpiresAt: now.Add(appAgentApprovalSeconds * time.Second).UnixMilli()}
	fields, _ := json.Marshal(appApprovalFields(app, usable.Install.ID, action, input, usable.TenancyKey, authority.owner()))
	if err := app.RunInTransaction(func(tx core.App) error {
		if _, err := tx.DB().Insert(appApprovalsTable, dbx.Params{
			"id": pending.ApprovalID, "install_id": usable.Install.ID, "action": action.Key, "person_id": authority.owner(), "input_hash": inputHash,
			"status": "pending", "created": now.UnixMilli(), "expires_at": pending.ExpiresAt, "connection_id": authority.ConnectionID,
			"tenancy": usable.TenancyKey, "preview": preview, "fields": string(fields),
		}).Execute(); err != nil {
			return err
		}
		return recordAppAudit(tx, usable.Install, "agent-approval-request", authority.owner(), map[string]any{
			"approval": pending.ApprovalID, "connection": authority.ConnectionID, "tool": action.Key,
		})
	}); err != nil {
		return appAgentPending{}, err
	}
	return pending, nil
}

var errAgentApprovalNotWaiting = errors.New("no such request is waiting")

// decideAgentApproval records the person's answer to a request still
// waiting — in their agent's program, or passed on from the conversation
// (in) — and allows the tool on the connection for an hour when they asked
// for that.
func decideAgentApproval(app core.App, approvalID, personID string, approve, forHour bool, in string) (storedAgentApproval, error) {
	status := "declined"
	if approve {
		status = "approved"
	}
	now := time.Now().UnixMilli()
	// The decision is kept with its line in the app's audit, or not at all.
	var stored storedAgentApproval
	var install storedAppInstall
	installFound := false
	if err := app.RunInTransaction(func(tx core.App) error {
		result, err := tx.DB().NewQuery(`UPDATE {{leafen_action_approvals}} SET [[status]] = {:status}, [[decided_at]] = {:now}
			WHERE [[id]] = {:id} AND [[person_id]] = {:person} AND [[connection_id]] != '' AND [[status]] = 'pending' AND [[expires_at]] > {:now}`).
			Bind(dbx.Params{"status": status, "now": now, "id": approvalID, "person": personID}).Execute()
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return errAgentApprovalNotWaiting
		}
		if stored, err = loadPersonAgentApproval(tx, approvalID, personID); err != nil {
			return err
		}
		install, err = loadAppInstall(tx, stored.InstallID)
		if errors.Is(err, errAppInstallNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		installFound = true
		detail := map[string]any{"approval": approvalID, "decision": strings.TrimSuffix(status, "d"), "tool": stored.Action,
			"connection": stored.ConnectionID}
		if in != "" {
			detail["in"] = in
		}
		return recordAppAudit(tx, install, "agent-approval", personID, detail)
	}); err != nil {
		return storedAgentApproval{}, err
	}
	if installFound {
		// "For an hour" allows the tool on this connection meanwhile.
		if approve && forHour {
			if err := allowAppActionForHour(app, install, approvalID, stored.Action, personID, stored.ConnectionID, stored.Tenancy); err != nil {
				return stored, err
			}
		}
	}
	return stored, nil
}

// expireAgentApprovals declines the requests nobody answered within a day.
func expireAgentApprovals(app core.App, now time.Time) error {
	expired := []storedAgentApproval{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[install_id]], [[action]], [[person_id]], [[connection_id]] FROM {{leafen_action_approvals}}
		WHERE [[connection_id]] != '' AND [[status]] = 'pending' AND [[expires_at]] <= {:now} LIMIT 500`).
		Bind(dbx.Params{"now": now.UnixMilli()}).All(&expired); err != nil {
		return err
	}
	for _, request := range expired {
		// A request expires with its line in the app's audit, or stays to
		// expire at the next turn.
		err := app.RunInTransaction(func(tx core.App) error {
			result, err := tx.DB().NewQuery(`UPDATE {{leafen_action_approvals}} SET [[status]] = 'expired', [[decided_at]] = {:now}
				WHERE [[id]] = {:id} AND [[status]] = 'pending'`).Bind(dbx.Params{"now": now.UnixMilli(), "id": request.ID}).Execute()
			if err != nil {
				return err
			}
			if count, _ := result.RowsAffected(); count != 1 {
				return nil
			}
			install, err := loadAppInstall(tx, request.InstallID)
			if errors.Is(err, errAppInstallNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			return recordAppAudit(tx, install, "agent-approval", "", map[string]any{"approval": request.ID, "decision": "expired",
				"tool": request.Action, "connection": request.ConnectionID, "person": request.PersonID})
		})
		if err != nil {
			return err
		}
	}
	return nil
}

const storedAgentApprovalColumns = `[[id]], [[install_id]], [[action]], [[person_id]], [[connection_id]], [[tenancy]], [[preview]], [[fields]], [[status]],
	[[created]], [[expires_at]], [[decided_at]]`

func loadPersonAgentApproval(app core.App, approvalID, personID string) (storedAgentApproval, error) {
	stored := storedAgentApproval{}
	err := app.DB().NewQuery(`SELECT ` + storedAgentApprovalColumns + ` FROM {{leafen_action_approvals}}
		WHERE [[id]] = {:id} AND [[person_id]] = {:person} AND [[connection_id]] != ''`).
		Bind(dbx.Params{"id": approvalID, "person": personID}).One(&stored)
	return stored, err
}
