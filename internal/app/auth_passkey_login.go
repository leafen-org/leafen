package app

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// The anonymous start allocates no database row. The server-sealed atomic
// library session is client/PKCE bound; only a successful login allocates its
// existing native session receipt. That receipt outlives the five-minute ticket.
type passkeyLoginTicket struct {
	Kind      string               `json:"kind"`
	ID        string               `json:"id"`
	Session   webauthn.SessionData `json:"session"`
	Challenge string               `json:"challenge"`
	AppReturn authAppReturn        `json:"appReturn"`
}

func beginPasskeyLogin(event *core.RequestEvent) error {
	rp, capability, err := configuredPasskeyRP()
	if err != nil {
		return passkeyAPIError(event, err)
	}
	input := passkeyLoginBeginInput{}
	if err := event.BindBody(&input); err != nil || !authAppChallengePattern.MatchString(input.Challenge) {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	appReturn, valid := parseAuthAppReturn(input.AppReturn, input.AppChallenge)
	if !valid {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	options, session, err := rp.BeginDiscoverableLogin(webauthn.WithLoginOrigin(capability.Origin))
	if err != nil {
		return passkeyAPIError(event, err)
	}
	ticket := passkeyLoginTicket{Kind: "passkey-login:v1", ID: security.RandomString(48), Session: *session, Challenge: input.Challenge, AppReturn: appReturn}
	sealed, err := sealPasskeyValue(event.App, ticket)
	if err != nil {
		return passkeyAPIError(event, err)
	}
	publicOptions, err := passkeyOptions(options.Response)
	if err != nil {
		return passkeyAPIError(event, err)
	}
	return event.JSON(http.StatusOK, passkeyCeremonyResponse{Ticket: sealed, Options: publicOptions})
}

func sealPasskeyValue(app core.App, value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return encryptAdministratorSetting(app, cipherPasskeyTicket, string(raw))
}

func openPasskeyValue(app core.App, value string, target any) error {
	if value == "" || len(value) > 32768 {
		return errPasskeyInvalid
	}
	raw, err := decryptAdministratorSetting(app, value)
	if err != nil {
		return errPasskeyInvalid
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return errPasskeyInvalid
	}
	return nil
}

func passkeyLoginIdentity(ticket passkeyLoginTicket, proof []byte, input passkeyLoginFinishInput) authSessionReplayIdentity {
	return authSessionReplayIdentity{
		RequestID:   "passkey-login:" + ticket.ID,
		RequestHash: hashAuthMFAValue(string(proof) + "\x00" + input.Verifier + "\x00" + strconv.FormatBool(input.ReplaceOldestSession)),
	}
}

func finishPasskeyLogin(event *core.RequestEvent) error {
	rp, capability, err := configuredPasskeyRP()
	if err != nil {
		return passkeyAPIError(event, err)
	}
	input := passkeyLoginFinishInput{}
	if err := event.BindBody(&input); err != nil {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	ticket := passkeyLoginTicket{}
	if err := openPasskeyValue(event.App, input.Ticket, &ticket); err != nil {
		return passkeyAPIError(event, err)
	}
	if ticket.Kind != "passkey-login:v1" || len(ticket.ID) != 48 || !authAppVerifierMatches(ticket.Challenge, input.Verifier) ||
		ticket.Session.Expires.IsZero() || !ticket.Session.Expires.After(time.Now()) || ticket.Session.Origin != capability.Origin || ticket.Session.RelyingPartyID != capability.RPID {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	proof, err := json.Marshal(input.Credential)
	if err != nil {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(proof)
	if err != nil {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	id := base64.RawURLEncoding.EncodeToString(parsed.RawID)
	if len(decodePasskeyID(id)) == 0 {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	replayIdentity := passkeyLoginIdentity(ticket, proof, input)
	response := sessionResponse{}
	ownerID := ""
	ownerTokenKeyHash := ""
	err = event.App.RunInTransaction(func(tx core.App) error {
		replay := replayIdentity
		row := storedPasskey{}
		err := tx.DB().NewQuery(`SELECT [[id]], [[user_id]], [[rp_id]], [[credential_json]], [[created_at]], [[last_used_at]] FROM {{leafen_passkeys}} WHERE [[id]] = {:id} AND [[rp_id]] = {:rp}`).Bind(dbx.Params{"id": id, "rp": capability.RPID}).One(&row)
		if errors.Is(err, sql.ErrNoRows) {
			return errPasskeyInvalid
		}
		if err != nil {
			return err
		}
		user, err := tx.FindRecordById("users", row.UserID)
		if err != nil {
			return err
		}
		if !bytes.Equal(parsed.Response.UserHandle, []byte(user.Id)) {
			return errPasskeyInvalid
		}
		ownerID = user.Id
		ownerTokenKeyHash = hashAuthMFAValue(user.TokenKey())
		// A consumed proof cannot renew admission across credential rotation,
		// even if the original session row has not yet been revoked.
		replay.RequestHash = hashAuthMFAValue(replay.RequestHash + "\x00" + user.TokenKey())
		if err := authorizePasskeySignIn(tx, user); err != nil {
			return err
		}
		var receiptOwner string
		err = tx.DB().NewQuery(`SELECT [[owner_id]] FROM {{leafen_auth_sessions}} WHERE [[request_id]] = {:request} AND [[request_id]] GLOB 'passkey-login:*' LIMIT 1`).Bind(dbx.Params{"request": replay.RequestID}).Row(&receiptOwner)
		if err == nil {
			if receiptOwner != user.Id {
				return errPasskeyInvalid
			}
			var found bool
			response, found, err = replayBrowserSession(tx, user, replay, time.Now().UTC().UnixMilli())
			if err != nil {
				return err
			}
			if !found {
				return errPasskeyInvalid
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, identity, err := accountPasskeysSnapshot(tx, user)
		if err != nil {
			return err
		}
		_, credential, err := rp.ValidatePasskeyLogin(func(rawID, handle []byte) (webauthn.User, error) {
			if !bytes.Equal(rawID, parsed.RawID) || !bytes.Equal(handle, []byte(user.Id)) {
				return nil, errPasskeyInvalid
			}
			return identity, nil
		}, ticket.Session, parsed)
		// Synced, backup-eligible authenticators can advance counters out of order.
		// A device-bound counter rollback is refused; signed UV and immutable
		// backup eligibility are still checked for every new assertion.
		if err != nil || (credential.Authenticator.CloneWarning && !credential.Flags.BackupEligible) {
			return errPasskeyInvalid
		}
		encoded, err := json.Marshal(credential)
		if err != nil {
			return err
		}
		if _, err := tx.DB().NewQuery(`UPDATE {{leafen_passkeys}} SET [[credential_json]] = {:json}, [[last_used_at]] = {:now}
			WHERE [[id]] = {:id} AND [[user_id]] = {:user}`).Bind(dbx.Params{"json": string(encoded), "now": time.Now().UTC().UnixMilli(), "id": id, "user": user.Id}).Execute(); err != nil {
			return err
		}
		response, err = issueAuthenticatedBrowserSessionWithReplayInTransaction(tx, user, browserSessionMetadataFromEvent(event), input.ReplaceOldestSession, "passkey", "", "passkey", replay)
		if err != nil {
			return err
		}
		return recordAuthAttemptInTransactionSuccess(tx, event, user.Id, user.Email(), authAuditMethodPasskeySignin, authAuditPhaseVerify)
	})
	if err != nil {
		failure := browserSessionFailure(err)
		if errors.Is(err, errPasskeyInvalid) || errors.Is(err, errAuthSessionRequestIDReused) {
			failure.Status, failure.Code, failure.Message = http.StatusBadRequest, errorCodePasskeyInvalid, "Start passkey login again."
		}
		if errors.Is(err, errSAMLRequired) {
			failure.Status, failure.Code, failure.Message = http.StatusForbidden, errorCodeSAMLRequired, "This organization requires SAML login."
		}
		if errors.Is(err, errLoginVerificationRequired) {
			failure.Status, failure.Code, failure.Message = http.StatusForbidden, errorCodeEmailVerificationRequired, "Email verification required."
		}
		return recordRejectedAuthAttemptWithCause(event, ownerID, "", authAuditMethodPasskeySignin, authAuditPhaseVerify, failure.Status, failure.Code, failure.Message, "auth.passkey", failure.Stage, failure.CauseCode, err)
	}
	if ticket.AppReturn.Address != "" {
		result, err := passkeyAppCompletion(event.App, ticket, response, ownerTokenKeyHash)
		if err != nil {
			return passkeyAPIError(event, err)
		}
		return event.JSON(http.StatusOK, result)
	}
	return event.JSON(http.StatusOK, passkeyLoginResponse{Session: &response})
}

func authorizePasskeySignIn(app core.App, user *core.Record) error {
	if err := authorizeAdministratorAccountSession(app, user.Id); err != nil {
		return err
	}
	if err := authorizeEnterpriseSSOSignIn(app, user.Id, "passkey", ""); err != nil {
		return err
	}
	settings, err := loadAdministratorSettings(app)
	if err != nil {
		return err
	}
	if settings.RequireEmailVerification {
		emails, err := verifiedAccountEmails(app, user.Id)
		if err != nil {
			return err
		}
		if len(emails) == 0 {
			return errLoginVerificationRequired
		}
	}
	return nil
}
