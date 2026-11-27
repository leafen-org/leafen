package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

var (
	errAuthSessionRequestIDReused = errors.New(errorCodeAuthSessionRequestIDReused)
	errAuthSessionReplayRace      = errors.New("AUTH_SESSION_REPLAY_RACE")
)

type authSessionReplayIdentity struct {
	RequestID   string
	RequestHash string
}

type storedAuthSessionReplay struct {
	ID          string `db:"id"`
	RequestHash string `db:"request_hash"`
	ExpiresAt   int64  `db:"expires_at"`
	RevokedAt   int64  `db:"revoked_at"`
}

func passwordLoginReplayIdentity(input loginInput) (authSessionReplayIdentity, bool) {
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" || utf8.RuneCountInString(requestID) > maxAuthLoginRequestIDRunes {
		return authSessionReplayIdentity{}, false
	}
	semantic := "password-login:v1:replace-oldest=" + strconv.FormatBool(input.ReplaceOldestSession)
	digest := sha256.Sum256([]byte(semantic))
	return authSessionReplayIdentity{
		RequestID: requestID, RequestHash: hex.EncodeToString(digest[:]),
	}, true
}

func issueAuthenticatedBrowserSessionWithReplayInTransaction(
	app core.App,
	user *core.Record,
	metadata browserSessionMetadata,
	replaceOldest bool,
	method string,
	providerID string,
	secondFactor string,
	replay authSessionReplayIdentity,
) (sessionResponse, error) {
	if err := authorizeAdministratorAccountSession(app, user.Id); err != nil {
		if errors.Is(err, errAdministratorAccountDisabled) {
			return sessionResponse{}, errAdministratorAccountDisabled
		}
		return sessionResponse{}, err
	}
	if err := authorizeEnterpriseSSOSignIn(app, user.Id, method, providerID); err != nil {
		return sessionResponse{}, err
	}
	if method == authSessionMethodDevice {
		if err := authorizeDeviceSignIn(app, user.Id); err != nil {
			return sessionResponse{}, err
		}
	}
	return issueBrowserSessionWithReplayInTransaction(app, user, metadata, replaceOldest, replay,
		authSessionProof{Method: method, SecondFactor: secondFactor})
}

// authSessionProof is how a session signed in and the second factor it went
// through ("totp" for a 2-step or recovery code, "passkey", "provider" for
// its sign-in provider's assurance), if any.
type authSessionProof struct {
	Method       string
	SecondFactor string
}

func issueBrowserSessionWithReplayInTransaction(
	app core.App,
	user *core.Record,
	metadata browserSessionMetadata,
	replaceOldest bool,
	replay authSessionReplayIdentity,
	proof authSessionProof,
) (sessionResponse, error) {
	response := sessionResponse{}
	now := time.Now().UTC()
	nowMillis := now.UnixMilli()
	if replay.RequestID != "" {
		replayed, found, err := replayBrowserSession(app, user, replay, nowMillis)
		if err != nil || found {
			return replayed, err
		}
	}

	var liveCount int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_auth_sessions}}
		WHERE [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0 AND [[expires_at]] > {:now}
	`).Bind(dbx.Params{"ownerId": user.Id, "now": nowMillis}).Row(&liveCount); err != nil {
		return response, err
	}
	if liveCount > authSessionLimit {
		return response, errAuthSessionCapacity
	}
	if liveCount == authSessionLimit {
		if !replaceOldest {
			return response, errAuthSessionCapacity
		}
		victimID, err := leastRecentlyUsedAuthSession(app, user.Id, nowMillis)
		if err != nil {
			return response, err
		}
		result, err := app.DB().NewQuery(`
			UPDATE {{leafen_auth_sessions}}
			SET [[revoked_at]] = {:now}, [[updated_at]] = {:now}
			WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId}
				AND [[revoked_at]] = 0 AND [[expires_at]] > {:now}
		`).Bind(dbx.Params{
			"id": victimID, "ownerId": user.Id, "now": nowMillis,
		}).Execute()
		if err != nil {
			return response, err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return response, refusedUnlessStorage(errAuthSessionCapacity, err)
		}
		recheckCollaborationAccessAfterCommit(app)
		endSignInWaits(app, user.Id)
	}

	sessionID := authSessionIDPrefix + security.RandomString(authSessionIDRandomLength)
	// A sign-in lasts as long without use as a renewal makes it last — a
	// device that joined with a code until it is cut off — and a managed
	// user's no longer than their organization's session duration.
	duration := authSessionIdleLifetime
	if proof.Method == authSessionMethodDevice {
		duration = authDeviceSessionLifetime
	}
	deadline, err := managedUserSessionDeadline(app, user, now)
	if err != nil {
		return response, err
	}
	if deadline > 0 && time.Duration(deadline-nowMillis)*time.Millisecond < duration {
		duration = time.Duration(deadline-nowMillis) * time.Millisecond
	}
	expiresAt := now.Add(duration).UnixMilli()
	token, err := newBrowserSessionToken(user, sessionID, duration)
	if err != nil {
		return response, err
	}
	secondFactor, providerSecondFactor := authSessionSecondFactorColumns(proof.SecondFactor)
	_, err = app.DB().Insert(authSessionsTable, dbx.Params{
		"id": sessionID, "owner_id": user.Id,
		"user_agent": metadata.UserAgent, "ip": metadata.IP,
		"expires_at": expiresAt, "rotated_at": nil, "revoked_at": 0, "deadline_at": deadline,
		"request_id": replay.RequestID, "request_hash": replay.RequestHash,
		"sign_in_method": boundedAuthSessionMetadata(proof.Method, 32), "second_factor": secondFactor,
		"provider_second_factor": providerSecondFactor, "created_by_connection": metadata.CreatedByConnection,
		"created_at": nowMillis, "updated_at": nowMillis,
	}).Execute()
	if err != nil {
		if replay.RequestID != "" && isSQLiteConstraintError(err) {
			return response, fmt.Errorf("%w: %v", errAuthSessionReplayRace, err)
		}
		return response, err
	}
	notifyAuthSessionCleanupAfterCommit(app)
	return browserSessionResponse(user, sessionID, token), nil
}

func recoverBrowserSessionReplay(
	app core.App,
	ownerID string,
	replay authSessionReplayIdentity,
) (sessionResponse, bool, error) {
	user, err := app.FindRecordById("users", ownerID)
	if err != nil {
		return sessionResponse{}, false, err
	}
	return replayBrowserSession(app, user, replay, time.Now().UTC().UnixMilli())
}

func replayBrowserSession(
	app core.App,
	user *core.Record,
	replay authSessionReplayIdentity,
	nowMillis int64,
) (sessionResponse, bool, error) {
	row := storedAuthSessionReplay{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[request_hash]], [[expires_at]], [[revoked_at]]
		FROM {{leafen_auth_sessions}}
		WHERE [[owner_id]] = {:ownerId} AND [[request_id]] = {:requestId}
			AND [[request_id]] <> ''
		LIMIT 1
	`).Bind(dbx.Params{"ownerId": user.Id, "requestId": replay.RequestID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return sessionResponse{}, false, nil
	}
	if err != nil {
		return sessionResponse{}, false, err
	}
	if row.RequestHash != replay.RequestHash || row.RevokedAt != 0 || row.ExpiresAt <= nowMillis {
		return sessionResponse{}, false, errAuthSessionRequestIDReused
	}
	remaining := time.Duration(row.ExpiresAt-nowMillis) * time.Millisecond
	token, err := newBrowserSessionToken(user, row.ID, remaining)
	if err != nil {
		return sessionResponse{}, false, err
	}
	return browserSessionResponse(user, row.ID, token), true, nil
}

func browserSessionResponse(user *core.Record, sessionID string, token string) sessionResponse {
	return sessionResponse{
		Token: token, SessionID: sessionID,
		User: sessionUserResponse{
			ID: user.Id, Email: user.Email(), Name: user.GetString("name"),
		},
	}
}
