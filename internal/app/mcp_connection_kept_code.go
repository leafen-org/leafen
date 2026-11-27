package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A connection kept in place of a connection text's code: the text's
// commands, having found a connection of this Leafen's that already reaches
// what the code gives (agent_connect_reuse.go), send its code here with that
// connection's own key; the code must be its person's, unexpired, and give
// no workspace the connection doesn't reach. Nothing is traded: the code
// stays unused for any other agent the same text goes to.

const (
	mcpOwnCodePath      = "/mcp/connections/current/code"
	mcpOwnCodeBodyBytes = 1024
)

type mcpOwnCodeInput struct {
	Code string `json:"code"`
}

type mcpOwnCodeView struct {
	Status     string                 `json:"status"`
	Workspaces []mcpWorkspaceListItem `json:"workspaces"`
	Message    string                 `json:"message"`
}

var errMCPOwnCodeBeyond = errors.New("This connection doesn't reach every workspace the code gives: trade the code for a new " +
	"connection instead.")

func registerMCPOwnCodeRoute(event *core.ServeEvent) {
	event.Router.POST(apiRoot+mcpOwnCodePath, keepOwnMCPConnectionForCode).Bind(apis.BodyLimit(mcpOwnCodeBodyBytes))
}

// keepOwnMCPConnectionForCode keeps the request's connection in place of the
// connection the code would make.
func keepOwnMCPConnectionForCode(event *core.RequestEvent) error {
	key, answered, err := ownMCPConnectionKey(event)
	if answered {
		return err
	}
	body, err := io.ReadAll(io.LimitReader(event.Request.Body, mcpOwnCodeBodyBytes+1))
	input := mcpOwnCodeInput{}
	if err != nil || len(body) > mcpOwnCodeBodyBytes || json.Unmarshal(body, &input) != nil {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionCodeInvalid, "This connection code is wrong, used or expired.")
	}
	if strings.TrimSpace(input.Code) == "" {
		return refuseMissingInput(event, errorCodeMCPConnectionCodeInvalid, "code", "the connection code your person's screen gave you.")
	}
	if !validMCPConnectionCode(strings.TrimSpace(input.Code)) {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionCodeInvalid, "This connection code is wrong, used or expired.")
	}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		stored := storedMCPConnectionCode{}
		err := txApp.DB().NewQuery(`SELECT [[id]], [[owner_id]], [[workspace_ids]]
			FROM {{leafen_mcp_connection_codes}} WHERE [[code_hash]] = {:codeHash} AND [[expires_at]] > {:now}`).
			Bind(dbx.Params{"codeHash": mcpTokenHash(strings.TrimSpace(input.Code)), "now": time.Now().UnixMilli()}).One(&stored)
		// Another account's code reads as a wrong one: it names nothing of theirs.
		if errors.Is(err, sql.ErrNoRows) || err == nil && stored.OwnerID != key.OwnerID {
			return errMCPConnectionCodeInvalid
		}
		if err != nil {
			return err
		}
		given := []string{}
		if err := json.Unmarshal([]byte(stored.WorkspaceIDs), &given); err != nil {
			return err
		}
		reached, err := loadMCPConnectionWorkspaces(txApp, key.ConnectionID)
		if err != nil {
			return err
		}
		for _, workspaceID := range given {
			if !slices.Contains(reached, workspaceID) {
				return errMCPOwnCodeBeyond
			}
		}
		return nil
	})
	switch {
	case errors.Is(err, errMCPConnectionCodeInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionCodeInvalid, "This connection code is wrong, used or expired.")
	case errors.Is(err, errMCPOwnCodeBeyond):
		return apiError(event, http.StatusForbidden, errorCodeMCPScopeDenied, errMCPOwnCodeBeyond.Error())
	case err != nil:
		return mcpTokenSettingsFailure(event, err)
	}
	list, err := connectionWorkspaceList(event.App, key.ConnectionID)
	if err != nil {
		return mcpTokenSettingsFailure(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, mcpOwnCodeView{Status: "kept", Workspaces: list,
		Message: "Kept: the connection you hold takes this text's place, so nothing was traded and the code stays unused."})
}
