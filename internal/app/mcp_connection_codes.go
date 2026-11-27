package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	mcpConnectionCodePrefix   = "lcc_"
	mcpConnectionCodeAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	mcpConnectionCodeLength   = 32
)

var (
	errMCPConnectionCodeInvalid  = errors.New(errorCodeMCPConnectionCodeInvalid)
	errMCPConnectionCodeCapacity = errors.New(errorCodeMCPConnectionCodeCapacity)
)

type storedMCPConnectionCode struct {
	ID           string `db:"id"`
	OwnerID      string `db:"owner_id"`
	ClientName   string `db:"client_name"`
	WorkspaceIDs string `db:"workspace_ids"`
	// The days of the life its key is to have; 0 for the default.
	ExpiresInDays int `db:"expires_in_days"`
	// When it was traded and for which connection; 0 and "" while unused.
	UsedAt       int64  `db:"used_at"`
	ConnectionID string `db:"connection_id"`
	UsedIP       string `db:"used_ip"`
}

// createMCPConnectionCode issues a code an agent trades once, within ten
// minutes, for an agent connection: its person's own rights in all their
// workspaces (connection_person_rights.go). What the key may do is checked
// now, so the person hears of a refusal before handing the code over, and
// again when it is traded.
func createMCPConnectionCode(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	var input mcpConnectionCodeInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionInvalid, "Check the MCP connection details.")
	}
	input.ClientName = strings.TrimSpace(input.ClientName)
	if input.ClientName == "" || utf8.RuneCountInString(input.ClientName) > 255 || !mcpTokenLifetimeKnown(input.ExpiresIn) {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionInvalid, "Check the MCP connection details.")
	}
	reached, err := agentConnectionWorkspaceIDs(event.App, user.Id)
	if errors.Is(err, errAgentConnectNoWorkspace) {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionInvalid, "You have no workspace for an agent to reach yet.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPConnectionFailed, "Couldn't create the connection code.", err)
	}
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPConnectionFailed, "Couldn't create the connection code.", err)
	}
	// A public address takes agents only over HTTPS; say so before a code is made.
	if _, err := newMCPConnectionURI(origin+mcpRoutePath, mcpConnectionCodeProbeKey); err != nil {
		return mcpManualCompletionError(event, err)
	}
	workspaceIDs, err := json.Marshal(reached)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPConnectionFailed, "Couldn't create the connection code.", err)
	}
	codeID := security.RandomString(15)
	code := mcpConnectionCodePrefix + security.RandomStringWithAlphabet(mcpConnectionCodeLength, mcpConnectionCodeAlphabet)
	now := time.Now().UnixMilli()
	expiresAt := now + int64(mcpConnectionCodeLifetimeMilliseconds)
	err = event.App.RunInTransaction(func(txApp core.App) error {
		// The key lives as long as chosen, a year unless chosen, from its
		// trade (mcp_token_lifetime.go); checked as from now.
		keyExpiresAt, err := mcpTokenExpiry(txApp, reached, now, input.ExpiresIn)
		if err != nil {
			return err
		}
		if err := authorizeManualMCPCompletion(txApp, user.Id, codeID, mcpConnectionInput{
			RequestID: "code:" + codeID, ClientName: input.ClientName, WorkspaceIDs: reached,
		}, now, keyExpiresAt); err != nil {
			return err
		}
		// Unused codes past their ten minutes are gone for good; used ones stay
		// as the record of their connection until credentials retire.
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_mcp_connection_codes}}
			WHERE [[owner_id]] = {:ownerId} AND (
				([[used_at]] = 0 AND [[expires_at]] <= {:now}) OR
				([[used_at]] > 0 AND [[used_at]] <= {:retired})
			)`).Bind(dbx.Params{
			"ownerId": user.Id, "now": now, "retired": now - int64(mcpCredentialRetentionMilliseconds),
		}).Execute(); err != nil {
			return err
		}
		var active int
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_mcp_connection_codes}}
			WHERE [[owner_id]] = {:ownerId} AND [[used_at]] = 0`).
			Bind(dbx.Params{"ownerId": user.Id}).Row(&active); err != nil {
			return err
		}
		if active >= maxActiveMCPConnectionCodesPerOwner {
			return errMCPConnectionCodeCapacity
		}
		// A code whose trade would find the connections full is refused now,
		// where the person sees why; unused codes take a place too.
		connections, err := countActiveMCPConnections(txApp, user.Id, now)
		if err != nil {
			return err
		}
		if connections+active >= maxActiveMCPConnectionsPerOwner {
			return errMCPConnectionCapacity
		}
		maker := ""
		if key, keyed := userKeyOf(event); keyed {
			maker = key.ConnectionID
		}
		if _, err = txApp.DB().Insert(mcpConnectionCodesTable, dbx.Params{
			"id": codeID, "owner_id": user.Id, "code_hash": mcpTokenHash(code),
			"client_name": input.ClientName, "workspace_ids": string(workspaceIDs),
			"scope":           personConnectionScopeName,
			"expires_in_days": mcpTokenLifetimeDays[input.ExpiresIn], "created_at": now, "expires_at": expiresAt,
			"created_by_connection": maker,
		}).Execute(); err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, errMCPConnectionCodeCapacity) {
		return apiError(event, http.StatusConflict, errorCodeMCPConnectionCodeCapacity, "Too many connection codes are waiting to be used. Try again in a few minutes.")
	}
	if err != nil {
		return mcpManualCompletionError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	// Whether the agent then asks how to start, judged as the code is made
	// (agent_start_offer.go).
	response := mcpConnectionCodeResponse{
		Code: code, ExchangeURL: origin + apiRoot + mcpConnectionCodeExchangePath,
		MCPURL: origin + nativeMCPPath, ExpiresAt: expiresAt, OfferStart: agentStartOffered(event.App, reached),
	}
	// An agent's own code says how it connects the folder it means (agent_connect_folder_by_code.go).
	if _, keyed := userKeyOf(event); keyed {
		response.Next = agentFolderConnectNext(event.App, origin, code, response.ExchangeURL, input.ClientName, user.Id)
	}
	return event.JSON(http.StatusCreated, response)
}

// exchangeMCPConnectionCode trades a code once for the key it promised, or for
// Leafen's entry with it as the program "answer" names takes one. The trade
// and the code's use commit together, so a refused trade leaves the code for
// another try. A used code trades again within its ten minutes only while
// the key it gave was never used, which that trade disconnects
// (mcp_connection_retrade.go); otherwise a second trade is refused.
func exchangeMCPConnectionCode(event *core.RequestEvent) error {
	var input mcpConnectionCodeExchangeInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionCodeInvalid, "This connection code is wrong, used or expired.")
	}
	if strings.TrimSpace(input.Code) == "" {
		return refuseMissingInput(event, errorCodeMCPConnectionCodeInvalid, "code", "the connection code your person's screen gave you.")
	}
	if !validMCPConnectionCode(strings.TrimSpace(input.Code)) {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionCodeInvalid, "This connection code is wrong, used or expired.")
	}
	// What it answers with is checked before the code is used (agent_connect_by_code.go).
	if !connectionCodeAnswerKnown(input.Answer) {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionInvalid, "answer is claude-code or codex, or left out for the key.")
	}
	origin, err := publicServerOrigin(event.Request)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPConnectionFailed, "Couldn't create the MCP connection.", err)
	}
	secret, err := mcpOAuthSecret(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPConnectionFailed, "Couldn't create the MCP connection.", err)
	}
	metadata := browserSessionMetadataFromEvent(event)
	var response mcpConnectionCodeExchangeResponse
	err = event.App.RunInTransaction(func(txApp core.App) error {
		now := time.Now().UnixMilli()
		stored := storedMCPConnectionCode{}
		err := txApp.DB().NewQuery(`
			SELECT [[id]], [[owner_id]], [[client_name]], [[workspace_ids]], [[expires_in_days]],
				[[used_at]], [[connection_id]], [[used_ip]]
			FROM {{leafen_mcp_connection_codes}}
			WHERE [[code_hash]] = {:codeHash} AND [[expires_at]] > {:now}
		`).Bind(dbx.Params{"codeHash": mcpTokenHash(strings.TrimSpace(input.Code)), "now": now}).One(&stored)
		if errors.Is(err, sql.ErrNoRows) {
			return errMCPConnectionCodeInvalid
		}
		if err != nil {
			return err
		}
		// The agent names itself as it trades the code, so agents connected
		// this way are told apart (2026-10-16: each was "에이전트"); without a
		// name, the one the code was made with, which says when.
		clientName := stored.ClientName
		if name := strings.TrimSpace(input.Name); name != "" && utf8.RuneCountInString(name) <= 255 &&
			!strings.ContainsFunc(name, unicode.IsControl) {
			clientName = name
		}
		// Traded again only by the program that traded it, from where it did
		// — not by another agent the same text went to.
		requestID := "code:" + stored.ID
		if stored.UsedAt > 0 {
			again := false
			if stored.UsedIP == metadata.IP {
				if again, err = takeBackUnusedMCPConnection(txApp, stored.OwnerID, stored.ConnectionID, clientName, now); err != nil {
					return err
				}
			}
			if !again {
				return errMCPConnectionCodeInvalid
			}
			requestID = mcpRetradeRequestID(requestID, now)
		}
		connection := mcpConnectionInput{
			RequestID: requestID, ClientName: clientName,
			ExpiresIn: mcpTokenLifetimeOfDays(stored.ExpiresInDays),
		}
		requestHash, err := mcpManualCompletionRequestHash(connection)
		if err != nil {
			return err
		}
		result := completeManualMCPConnection(txApp, stored.OwnerID, connection, requestHash, origin+mcpRoutePath, secret)
		if result.err != nil {
			return result.err
		}
		claimed, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_mcp_connection_codes}}
			SET [[used_at]] = {:now}, [[connection_id]] = {:connectionId}, [[used_ip]] = {:ip}, [[used_user_agent]] = {:userAgent}
			WHERE [[id]] = {:id} AND [[used_at]] = {:usedAt}`).Bind(dbx.Params{
			"id": stored.ID, "now": now, "connectionId": result.response.ID, "usedAt": stored.UsedAt,
			"ip": metadata.IP, "userAgent": metadata.UserAgent,
		}).Execute()
		if err != nil {
			return err
		}
		// The key a code a connection made trades for ends with that connection.
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_mcp_connections}} SET [[created_by_connection]] = (
				SELECT [[created_by_connection]] FROM {{leafen_mcp_connection_codes}} WHERE [[id]] = {:code})
			WHERE [[id]] = {:connectionId} AND [[created_by_connection]] = ''`).Bind(dbx.Params{
			"code": stored.ID, "connectionId": result.response.ID,
		}).Execute(); err != nil {
			return err
		}
		if rows, err := claimed.RowsAffected(); err != nil || rows != 1 {
			return mcpRefusedUnlessStorage(errMCPConnectionCodeInvalid, err)
		}
		response = mcpConnectionCodeExchangeResponse{
			ConnectionID: result.response.ID, ClientName: clientName, WorkspaceIDs: result.response.WorkspaceIDs,
			ExpiresAt: result.response.ExpiresAt, MCPURL: origin + nativeMCPPath,
			Key: deterministicMCPToken(secret, "manual-access", result.response.ID, "lmc_"),
		}
		return nil
	})
	if errors.Is(err, errMCPConnectionCodeInvalid) {
		return apiError(event, http.StatusBadRequest, errorCodeMCPConnectionCodeInvalid, "This connection code is wrong, used or expired.")
	}
	if err != nil {
		return mcpManualCompletionError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	if input.Answer != "" {
		return writeConnectionCodeAnswer(event, input.Answer, response.MCPURL, response.Key)
	}
	return event.JSON(http.StatusCreated, response)
}

// The shape of a key, only to ask whether the server's address takes one.
var mcpConnectionCodeProbeKey = "lmc_" + strings.Repeat("0", 43)

func validMCPConnectionCode(code string) bool {
	if len(code) != len(mcpConnectionCodePrefix)+mcpConnectionCodeLength || !strings.HasPrefix(code, mcpConnectionCodePrefix) {
		return false
	}
	for _, value := range code[len(mcpConnectionCodePrefix):] {
		if !strings.ContainsRune(mcpConnectionCodeAlphabet, value) {
			return false
		}
	}
	return true
}
