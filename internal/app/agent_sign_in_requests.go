package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A browser signs in by the person's agent (user decision 2026-11-16, from
// the report of a person's agent moving their Notion: people didn't know
// which of the QR and the code to press, and an agent's own browser stood at
// the sign-in screen). As a device's sign-in in OAuth (RFC 8628), turned
// round: the sign-in screen asks with no account and shows a short code; the
// person gives it to an agent already connected — or, in a browser the agent
// works in, the agent presses for it there itself after asking them once
// (2026-11-22, the user) —, which shows them the
// browser's device, address and time (leafen_sign_in_requests, asking them
// as person_ask.go does) and approves it as the person it works for. The
// agent never makes a code: it approves only one a screen shows, so an agent
// steered by what it reads can't sign anything in. The code lasts five
// minutes and is used once; only the browser that asked — holding a secret
// of its own besides the code — takes the sign-in, which lasts as a
// device's does. The approval lands in the person's inbox, and the person
// can turn approvals by agents off in Settings › Account.

const (
	agentSignInCodeLength = 6
	// Letters and digits a person reads out, without I, L and O: a typed one
	// reads as 1 or 0 (readTypedCode).
	agentSignInCodeAlphabet    = "0123456789ABCDEFGHJKMNPQRSTUVWXYZ"
	agentSignInLifetime        = 5 * time.Minute
	agentSignInClaimWindow     = 2 * time.Minute
	agentSignInPollInterval    = 2 * time.Second
	agentSignInTokenPrefix     = "lsb_"
	agentSignInTokenLength     = 40
	agentSignInWaitingOnServer = 256
	agentSignInRecordKept      = 30 * 24 * time.Hour
	agentSignInOriginLength    = 255
	agentSignInPath            = "/auth/agent-sign-in"
	agentSignInSettingPath     = "/account/agent-sign-in"
	agentSignInLimiterStoreKey = "leafen.agentSignInLimiter"
	agentSignInNoticePage      = agentNoticePagePrefix + "sign-in"
	agentSignInNoticeKind      = "agent-sign-in:"
)

var errAgentSignInMissing = errors.New(errorCodeDeviceCodeInvalid)

type agentSignInLimiter struct {
	asks         *requestWindowLimiter
	wrongTokens  *requestWindowLimiter
	wrongCodes   *requestWindowLimiter
	serverWrongs *requestWindowLimiter
}

func agentSignInLimiterOf(app core.App) *agentSignInLimiter {
	return app.Store().GetOrSet(agentSignInLimiterStoreKey, func() any {
		return &agentSignInLimiter{
			asks:         newRequestWindowLimiter(20, 15*time.Minute),
			wrongTokens:  newRequestWindowLimiter(30, 15*time.Minute),
			wrongCodes:   newRequestWindowLimiter(10, 15*time.Minute),
			serverWrongs: newRequestWindowLimiter(200, time.Hour),
		}
	}).(*agentSignInLimiter)
}

func registerAgentSignInRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+agentSignInPath, askAgentSignIn).Bind(apis.BodyLimit(4 << 10))
	event.Router.POST(apiRoot+agentSignInPath+"/poll", pollAgentSignIn).Bind(apis.BodyLimit(4 << 10))
	event.Router.POST(apiRoot+agentSignInPath+"/cancel", cancelAgentSignIn).Bind(apis.BodyLimit(4 << 10))
	event.Router.GET(apiRoot+agentSignInSettingPath, readAgentSignInSetting)
	event.Router.PUT(apiRoot+agentSignInSettingPath, setAgentSignInSetting).Bind(apis.BodyLimit(1 << 10))
}

// agentSignInRequest is one browser's request as it waits.
type agentSignInRequest struct {
	ID        string `db:"id"`
	RequestIP string `db:"request_ip"`
	UserAgent string `db:"request_user_agent"`
	// The page's origin, which tells Leafen's own app from a browser (clientDeviceSummary).
	Origin    string `db:"request_origin"`
	Status    string `db:"status"`
	CreatedAt int64  `db:"created_at"`
	ExpiresAt int64  `db:"expires_at"`
	OwnerID   string `db:"owner_id"`
}

const agentSignInColumns = `[[id]], [[request_ip]], [[request_user_agent]], [[request_origin]], [[status]], [[created_at]], [[expires_at]], [[owner_id]]`

type agentSignInStart struct {
	Code      string `json:"code"`
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expiresAt"`
	Interval  int    `json:"interval"`
}

type agentSignInTokenInput struct {
	Token                string `json:"token"`
	ReplaceOldestSession bool   `json:"replaceOldestSession"`
}

// agentSignInWait is a request not signed in yet: pending (until
// expiresAt), declined, or expired.
type agentSignInWait struct {
	Status    string `json:"status"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
}

// agentSignInCodeShown is a code as the screen shows it, in threes.
func agentSignInCodeShown(code string) string {
	return code[:3] + "-" + code[3:]
}

// askAgentSignIn takes a browser's request to be signed in by an agent,
// with no account: its code to show and the secret it takes the sign-in with.
func askAgentSignIn(event *core.RequestEvent) error {
	limiter := agentSignInLimiterOf(event.App)
	address := "address:" + requestLimitAddress(event.RealIP())
	if admitted, _ := limiter.asks.admit(address, time.Now()); !admitted {
		return deviceCodeAPIError(event, errDeviceCodeLimited)
	}
	token := agentSignInTokenPrefix + security.RandomString(agentSignInTokenLength)
	code := ""
	now := time.Now().UnixMilli()
	expiresAt := now + agentSignInLifetime.Milliseconds()
	err := event.App.RunInTransaction(func(txApp core.App) error {
		// Requests left waiting go a day after they ran out; decided ones
		// stay a month as the record of what they signed in.
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_agent_sign_in_requests}}
			WHERE ([[status]] IN ('pending', 'approved') AND [[expires_at]] <= {:stale})
				OR ([[status]] IN ('declined', 'used') AND [[decided_at]] <= {:retired})`).
			Bind(dbx.Params{"stale": now - (24 * time.Hour).Milliseconds(), "retired": now - agentSignInRecordKept.Milliseconds()}).
			Execute(); err != nil {
			return err
		}
		waiting := 0
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_agent_sign_in_requests}}
			WHERE [[status]] = 'pending' AND [[expires_at]] > {:now}`).Bind(dbx.Params{"now": now}).Row(&waiting); err != nil {
			return err
		}
		if waiting >= agentSignInWaitingOnServer {
			return errDeviceCodeLimited
		}
		// No two requests waiting share a code.
		for attempt := 0; code == "" && attempt < 8; attempt++ {
			candidate := security.RandomStringWithAlphabet(agentSignInCodeLength, agentSignInCodeAlphabet)
			if _, found, err := findWaitingAgentSignIn(txApp, candidate); err != nil {
				return err
			} else if !found {
				code = candidate
			}
		}
		if code == "" {
			return errDeviceCodeLimited
		}
		_, err := txApp.DB().Insert(agentSignInRequestsTable, dbx.Params{
			"id": "las_" + security.RandomString(20), "browser_token_hash": mcpTokenHash(token), "code_hash": mcpTokenHash(code),
			"request_ip":         boundedAuthSessionMetadata(event.RealIP(), maxAuthSessionIPLength),
			"request_user_agent": boundedAuthSessionMetadata(event.Request.UserAgent(), maxAuthSessionUserAgentLength),
			"request_origin":     boundedAuthSessionMetadata(event.Request.Header.Get("Origin"), agentSignInOriginLength),
			"status":             "pending", "created_at": now, "expires_at": expiresAt,
		}).Execute()
		return err
	})
	if errors.Is(err, errDeviceCodeLimited) {
		limiter.asks.refund(address)
		return deviceCodeAPIError(event, errDeviceCodeLimited)
	}
	if err != nil {
		return deviceCodeAPIError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusCreated, agentSignInStart{Code: codeWithServerCode(event.App, agentSignInCodeShown(code)), Token: token, ExpiresAt: expiresAt,
		Interval: int(agentSignInPollInterval.Seconds())})
}

// pollAgentSignIn answers the browser that asked: still waiting, declined,
// ran out, or — once approved — its sign-in, made once.
func pollAgentSignIn(event *core.RequestEvent) error {
	input := agentSignInTokenInput{}
	_ = event.BindBody(&input)
	limiter := agentSignInLimiterOf(event.App)
	address := "address:" + requestLimitAddress(event.RealIP())
	if admitted, _ := limiter.wrongTokens.admit(address, time.Now()); !admitted {
		return deviceCodeAPIError(event, errDeviceCodeLimited)
	}
	token := strings.TrimSpace(input.Token)
	stored := agentSignInRequest{}
	err := event.App.DB().NewQuery(`SELECT ` + agentSignInColumns + ` FROM {{leafen_agent_sign_in_requests}}
		WHERE [[browser_token_hash]] = {:hash}`).Bind(dbx.Params{"hash": mcpTokenHash(token)}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) || token == "" {
		return deviceCodeAPIError(event, errDeviceCodeInvalid)
	}
	if err != nil {
		return deviceCodeAPIError(event, err)
	}
	limiter.wrongTokens.refund(address)
	now := time.Now().UnixMilli()
	event.Response.Header().Set("Cache-Control", "no-store")
	switch {
	case stored.Status == "used":
		return deviceCodeAPIError(event, errDeviceCodeInvalid)
	case stored.Status == "declined":
		return event.JSON(http.StatusOK, agentSignInWait{Status: "declined"})
	case stored.ExpiresAt <= now:
		return event.JSON(http.StatusOK, agentSignInWait{Status: "expired"})
	case stored.Status == "pending":
		return event.JSON(http.StatusOK, agentSignInWait{Status: "pending", ExpiresAt: stored.ExpiresAt})
	}
	return claimAgentSignIn(event, stored, input.ReplaceOldestSession)
}

// claimAgentSignIn signs the browser in as the person whose agent approved
// it; the sign-in's making and the request's use commit together. At the
// account's limit the request stays approved and the browser may end the
// account's oldest sign-in for it, as signing in by a device code does.
func claimAgentSignIn(event *core.RequestEvent, stored agentSignInRequest, replaceOldest bool) error {
	metadata := browserSessionMetadataFromEvent(event)
	session := sessionResponse{}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		user, err := txApp.FindRecordById("users", stored.OwnerID)
		if err != nil {
			return refusedUnlessStorage(errDeviceCodeInvalid, err)
		}
		session, err = issueAuthenticatedBrowserSessionInTransaction(txApp, user, metadata, replaceOldest, authSessionMethodDevice, "", "")
		if err != nil {
			return err
		}
		claimed, err := txApp.DB().NewQuery(`UPDATE {{leafen_agent_sign_in_requests}}
			SET [[status]] = 'used', [[session_id]] = {:session}, [[used_at]] = {:now}
			WHERE [[id]] = {:id} AND [[status]] = 'approved' AND [[expires_at]] > {:now}`).
			Bind(dbx.Params{"id": stored.ID, "session": session.SessionID, "now": time.Now().UnixMilli()}).Execute()
		if err != nil {
			return err
		}
		if rows, err := claimed.RowsAffected(); err != nil || rows != 1 {
			return refusedUnlessStorage(errDeviceCodeInvalid, err)
		}
		return recordAuthAttemptInTransactionSuccessWithMetadata(
			txApp, user.Id, user.Email(), authAuditMethodDeviceCode, authAuditPhaseVerify, metadata,
		)
	})
	if err != nil {
		if _, auditErr := recordAuthAttempt(event.App, authAuditAttempt{
			OwnerID: stored.OwnerID, Method: authAuditMethodDeviceCode, Phase: authAuditPhaseVerify,
			Outcome: authAuditOutcomeFailure, ReasonCode: deviceCodeFailureCode(err), ReasonStatus: deviceCodeFailureStatus(err),
			Metadata: metadata,
		}); auditErr != nil {
			event.App.Logger().Warn("leafen agent sign-in audit failed", "error", auditErr)
		}
		return deviceCodeAPIError(event, err)
	}
	recheckCollaborationAccessAfterCommit(event.App)
	return event.JSON(http.StatusCreated, session)
}

// cancelAgentSignIn ends a request not decided yet, as when its screen is left.
func cancelAgentSignIn(event *core.RequestEvent) error {
	input := agentSignInTokenInput{}
	_ = event.BindBody(&input)
	if token := strings.TrimSpace(input.Token); token != "" {
		if _, err := event.App.DB().NewQuery(`DELETE FROM {{leafen_agent_sign_in_requests}}
			WHERE [[browser_token_hash]] = {:hash} AND [[status]] = 'pending'`).
			Bind(dbx.Params{"hash": mcpTokenHash(token)}).Execute(); err != nil {
			return deviceCodeAPIError(event, err)
		}
	}
	return event.NoContent(http.StatusNoContent)
}

// normalizeAgentSignInCode reads a request's code as typed, said or pasted,
// with or without the server's code before it (server_code.go).
func normalizeAgentSignInCode(typed string) (string, bool) {
	code, _ := splitServerCode(typed, agentSignInCodeLength)
	if len(code) != agentSignInCodeLength || strings.ContainsFunc(code, func(r rune) bool { return !strings.ContainsRune(agentSignInCodeAlphabet, r) }) {
		return "", false
	}
	return code, true
}

// findWaitingAgentSignIn is the request waiting under code; none for a code
// another server's code begins.
func findWaitingAgentSignIn(app core.App, typed string) (agentSignInRequest, bool, error) {
	code, ok := normalizeAgentSignInCode(typed)
	if otherServerCode(app, typed, agentSignInCodeLength) != "" {
		ok = false
	}
	if !ok {
		return agentSignInRequest{}, false, nil
	}
	found := agentSignInRequest{}
	err := app.DB().NewQuery(`SELECT ` + agentSignInColumns + ` FROM {{leafen_agent_sign_in_requests}}
		WHERE [[code_hash]] = {:hash} AND [[status]] = 'pending' AND [[expires_at]] > {:now}`).
		Bind(dbx.Params{"hash": mcpTokenHash(code), "now": time.Now().UnixMilli()}).One(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return found, false, nil
	}
	return found, err == nil, err
}

// decideAgentSignIn keeps the answer the person gave through their agent's
// connection; approved, the browser has a little while to take it, and the
// person's inbox says so.
func decideAgentSignIn(app core.App, request agentSignInRequest, yes bool, ownerID, clientName, byConnection, in string) error {
	now := time.Now().UnixMilli()
	status, until := "declined", request.ExpiresAt
	if yes {
		status, until = "approved", now+agentSignInClaimWindow.Milliseconds()
	}
	result, err := app.DB().NewQuery(`UPDATE {{leafen_agent_sign_in_requests}} SET [[status]] = {:status}, [[owner_id]] = {:owner},
		[[client_name]] = {:client}, [[decided_by_connection]] = {:by}, [[decided_in]] = {:in}, [[decided_at]] = {:now}, [[expires_at]] = {:until}
		WHERE [[id]] = {:id} AND [[status]] = 'pending' AND [[expires_at]] > {:now}`).Bind(dbx.Params{
		"id": request.ID, "status": status, "owner": ownerID, "client": appOneLine(clientName, 255), "by": byConnection, "in": in,
		"now": now, "until": until,
	}).Execute()
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return errAgentSignInMissing
	}
	if yes {
		notifyAgentSignInApproved(app, request, ownerID, clientName)
	}
	return nil
}

var (
	agentSignInNoticeTitle = mcpWords{"새 기기 로그인 승인됨 (%s 경유)", "New device login approved (through %s)"}
	agentSignInAgentless   = mcpWords{"에이전트", "an agent"}
)

// agentSignInDevice is a request's browser — or Leafen's app — and system,
// address and its country, as the person reads them in their language.
func agentSignInDevice(request agentSignInRequest, language string) string {
	place := request.RequestIP
	if country := deviceCountry(request.RequestIP); country != "" {
		place = strings.TrimSpace(place + " (" + country + ")")
	}
	return strings.Trim(strings.Join([]string{agentSignInRequestDevice(request, language), place}, " · "), " ·")
}

// agentSignInRequestDevice names the browser or app that asked (clientDeviceSummary).
func agentSignInRequestDevice(request agentSignInRequest, language string) string {
	return clientDeviceSummary(request.UserAgent, request.Origin, language)
}

// notifyAgentSignInApproved puts the approval in the person's inbox — and so
// in their mail and push as their settings say — naming the agent and the
// browser; it opens Settings › Account, where the device can be signed out.
func notifyAgentSignInApproved(app core.App, request agentSignInRequest, ownerID, clientName string) {
	workspaceID, err := personNoticeWorkspace(app, ownerID, "")
	if err != nil || workspaceID == "" {
		return
	}
	language, err := serverTextLanguage(app, ownerID, nil)
	if err != nil {
		language = accountLanguageEn
	}
	agent := firstNonempty(appOneLine(clientName, 80), agentSignInAgentless.in(language))
	now := time.Now().UnixMilli()
	if _, err := upsertProductNotifications(context.Background(), app, []productNotificationProjection{{
		ID: appNoticeID("agent-sign-in", request.ID, ownerID), EventID: appNoticeID("agent-sign-in-event", request.ID),
		WorkspaceID: workspaceID, UserID: ownerID, ActivityKey: agentSignInNoticeKind + request.ID, Kind: "system",
		PageID: agentSignInNoticePage, Title: fmt.Sprintf(agentSignInNoticeTitle.in(language), agent),
		Preview: appOneLine(agentSignInDevice(request, language), 300), Target: "/settings?section=account", OccurredAt: now, Personal: true,
	}}, now); err != nil {
		app.Logger().Warn("leafen agent sign-in notice failed", "request", request.ID, "error", err)
	}
}

// agentSignInTurnsOn says a key's request to the setting turns approvals by
// agents back on, which waits for its person's yes; a body that isn't one
// the handler refuses before asking.
func agentSignInTurnsOn(app core.App, key userKeyAuthority, body []byte) bool {
	input := agentSignInSetting{}
	if json.Unmarshal(body, &input) != nil {
		return true
	}
	if input.Allowed == nil || !*input.Allowed {
		return false
	}
	off, err := agentSignInTurnedOff(app, key.OwnerID)
	return err != nil || off
}

// agentSignInTurnedOff says whether the person turned approvals by agents off.
func agentSignInTurnedOff(app core.App, userID string) (bool, error) {
	count := 0
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_agent_sign_in_off}} WHERE [[user_id]] = {:user}`).
		Bind(dbx.Params{"user": userID}).Row(&count)
	return count > 0, err
}

type agentSignInSetting struct {
	Allowed *bool `json:"allowed"`
}

// readAgentSignInSetting answers whether the person's agents may approve a
// browser's sign-in; on unless they turned it off. The person changes it on
// their screen, or their agent in their conversation as "모두 맡기기" is
// changed (mcp_token_hand_over.go): turning it on asks them once
// (agentSignInTurnsOn), turning it off goes ahead at once.
func readAgentSignInSetting(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	off, err := agentSignInTurnedOff(event.App, user.Id)
	if err != nil {
		return deviceCodeAPIError(event, err)
	}
	allowed := !off
	return event.JSON(http.StatusOK, agentSignInSetting{Allowed: &allowed})
}

func setAgentSignInSetting(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := agentSignInSetting{}
	if err := event.BindBody(&input); err != nil || input.Allowed == nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Say whether agents may approve logins, as allowed.")
	}
	// Turning it on with a key asks its person once the request reads.
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	query := `DELETE FROM {{leafen_agent_sign_in_off}} WHERE [[user_id]] = {:user}`
	if !*input.Allowed {
		query = `INSERT INTO {{leafen_agent_sign_in_off}} ([[user_id]], [[turned_off_at]]) VALUES ({:user}, {:now})
			ON CONFLICT ([[user_id]]) DO NOTHING`
	}
	if _, err := event.App.DB().NewQuery(query).Bind(dbx.Params{"user": user.Id, "now": time.Now().UnixMilli()}).Execute(); err != nil {
		return deviceCodeAPIError(event, err)
	}
	return event.JSON(http.StatusOK, input)
}
