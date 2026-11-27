package app

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

type storedPasskeyRegistration struct {
	ID           string `db:"id"`
	RequestID    string `db:"request_id"`
	SessionID    string `db:"session_id"`
	TokenKeyHash string `db:"token_key_hash"`
	BaseRevision string `db:"base_revision"`
	SessionJSON  string `db:"session_json"`
	OptionsJSON  string `db:"options_json"`
	ExpiresAt    int64  `db:"expires_at"`
	CredentialID string `db:"credential_id"`
	ResponseHash string `db:"response_hash"`
}

func loadPasskeyRegistration(app core.App, userID string) (storedPasskeyRegistration, error) {
	row := storedPasskeyRegistration{}
	err := app.DB().NewQuery(`SELECT [[id]], [[request_id]], [[session_id]], [[token_key_hash]], [[base_revision]], [[session_json]], [[options_json]], [[expires_at]], [[credential_id]], [[response_hash]]
		FROM {{leafen_passkey_registrations}} WHERE [[user_id]] = {:userId}`).Bind(dbx.Params{"userId": userID}).One(&row)
	return row, err
}

func beginPasskeyRegistration(event *core.RequestEvent) error {
	rp, capability, err := configuredPasskeyRP()
	if err != nil {
		return passkeyAPIError(event, err)
	}
	input := passkeyRegistrationBeginInput{}
	if err := event.BindBody(&input); err != nil || input.RequestID == "" || len(input.RequestID) > 128 {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	response := passkeyCeremonyResponse{}
	err = event.App.RunInTransaction(func(tx core.App) error {
		user, sessionID, err := currentPasskeyAccount(event, tx)
		if err != nil {
			return err
		}
		state, identity, err := accountPasskeysSnapshot(tx, user)
		if err != nil {
			return err
		}
		if state.SSORequired {
			return errSAMLRequired
		}
		if state.Revision != input.BaseRevision {
			return errPasskeyConflict
		}
		if !state.CanAdd {
			return errPasskeyLimit
		}
		now := time.Now().UTC()
		previous, err := loadPasskeyRegistration(tx, user.Id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && previous.RequestID == input.RequestID {
			if previous.BaseRevision != state.Revision || previous.SessionID != sessionID || previous.TokenKeyHash != hashAuthMFAValue(user.TokenKey()) || previous.ExpiresAt <= now.UnixMilli() || previous.CredentialID != "" {
				return errPasskeyConflict
			}
			response.Ticket = previous.ID
			return json.Unmarshal([]byte(previous.OptionsJSON), &response.Options)
		}
		excluded := []protocol.CredentialDescriptor{}
		for _, credential := range identity.credentials {
			excluded = append(excluded, credential.Descriptor())
		}
		creation, session, err := rp.BeginRegistration(identity, webauthn.WithExclusions(excluded), webauthn.WithRegistrationOrigin(capability.Origin))
		if err != nil {
			return err
		}
		options, err := passkeyOptions(creation.Response)
		if err != nil {
			return err
		}
		sessionJSON, err := json.Marshal(session)
		if err != nil {
			return err
		}
		optionsJSON, err := json.Marshal(options)
		if err != nil {
			return err
		}
		response = passkeyCeremonyResponse{Ticket: security.RandomString(48), Options: options}
		_, err = tx.DB().NewQuery(`INSERT INTO {{leafen_passkey_registrations}}
			([[user_id]], [[id]], [[request_id]], [[session_id]], [[token_key_hash]], [[base_revision]], [[session_json]], [[options_json]], [[expires_at]], [[credential_id]], [[response_hash]])
			VALUES ({:user}, {:id}, {:request}, {:session}, {:key}, {:revision}, {:json}, {:options}, {:expires}, '', '')
			ON CONFLICT ([[user_id]]) DO UPDATE SET [[id]]=excluded.[[id]], [[request_id]]=excluded.[[request_id]], [[session_id]]=excluded.[[session_id]],
				[[token_key_hash]]=excluded.[[token_key_hash]], [[base_revision]]=excluded.[[base_revision]], [[session_json]]=excluded.[[session_json]],
				[[options_json]]=excluded.[[options_json]], [[expires_at]]=excluded.[[expires_at]], [[credential_id]]='', [[response_hash]]=''`).Bind(dbx.Params{
			"user": user.Id, "id": response.Ticket, "request": input.RequestID, "session": sessionID, "key": hashAuthMFAValue(user.TokenKey()),
			"revision": state.Revision, "json": string(sessionJSON), "options": string(optionsJSON), "expires": session.Expires.UnixMilli(),
		}).Execute()
		return err
	})
	if err != nil {
		return passkeyAPIError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}

func finishPasskeyRegistration(event *core.RequestEvent) error {
	rp, capability, err := configuredPasskeyRP()
	if err != nil {
		return passkeyAPIError(event, err)
	}
	input := passkeyRegistrationFinishInput{}
	if err := event.BindBody(&input); err != nil || len(input.Ticket) != 48 {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	proof, err := json.Marshal(input.Credential)
	if err != nil {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(proof)
	if err != nil {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	responseHash := hashAuthMFAValue(string(proof))
	state := accountPasskeyState{}
	owner := ""
	err = event.App.RunInTransaction(func(tx core.App) error {
		user, sessionID, err := currentPasskeyAccount(event, tx)
		if err != nil {
			return err
		}
		owner = user.Id
		row, err := loadPasskeyRegistration(tx, user.Id)
		if errors.Is(err, sql.ErrNoRows) {
			return errPasskeyInvalid
		}
		if err != nil {
			return err
		}
		if row.ID != input.Ticket || row.SessionID != sessionID || row.TokenKeyHash != hashAuthMFAValue(user.TokenKey()) || row.ExpiresAt <= time.Now().UTC().UnixMilli() {
			return errPasskeyInvalid
		}
		identity := passkeyUser{}
		state, identity, err = accountPasskeysSnapshot(tx, user)
		if err != nil {
			return err
		}
		if state.SSORequired {
			return errSAMLRequired
		}
		if row.CredentialID != "" {
			if row.ResponseHash != responseHash {
				return errPasskeyConflict
			}
			for _, entry := range state.Entries {
				if entry.ID == row.CredentialID {
					return nil
				}
			}
			return errPasskeyInvalid
		}
		if row.BaseRevision != state.Revision {
			return errPasskeyConflict
		}
		if !state.CanAdd {
			return errPasskeyLimit
		}
		session := webauthn.SessionData{}
		if err := json.Unmarshal([]byte(row.SessionJSON), &session); err != nil {
			return err
		}
		if session.Origin != capability.Origin || session.RelyingPartyID != capability.RPID {
			return errPasskeyInvalid
		}
		credential, err := rp.CreateCredential(identity, session, parsed)
		if err != nil || len(credential.ID) == 0 || len(credential.ID) > 1023 {
			return errPasskeyInvalid
		}
		id := base64.RawURLEncoding.EncodeToString(credential.ID)
		encoded, err := json.Marshal(credential)
		if err != nil {
			return err
		}
		if _, err := tx.DB().Insert("leafen_passkeys", dbx.Params{"id": id, "user_id": user.Id, "rp_id": capability.RPID, "credential_json": string(encoded), "created_at": time.Now().UTC().UnixMilli(), "last_used_at": 0}).Execute(); err != nil {
			if isSQLiteConstraintError(err) {
				return errPasskeyConflict
			}
			return err
		}
		if _, err := tx.DB().NewQuery(`UPDATE {{leafen_passkey_registrations}} SET [[credential_id]] = {:credential}, [[response_hash]] = {:hash}
			WHERE [[user_id]] = {:user} AND [[id]] = {:id}`).Bind(dbx.Params{"credential": id, "hash": responseHash, "user": user.Id, "id": row.ID}).Execute(); err != nil {
			return err
		}
		state, _, err = accountPasskeysSnapshot(tx, user)
		return err
	})
	if err != nil {
		return passkeyAPIError(event, err)
	}
	noticeAccountSecurityChange(event, owner, securityChangePasskeyAdded, "")
	return event.JSON(http.StatusOK, state)
}
