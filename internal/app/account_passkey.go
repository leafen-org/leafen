package app

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedPasskey struct {
	ID             string `db:"id"`
	UserID         string `db:"user_id"`
	RPID           string `db:"rp_id"`
	CredentialJSON string `db:"credential_json"`
	CreatedAt      int64  `db:"created_at"`
	LastUsedAt     int64  `db:"last_used_at"`
}

func accountPasskeysSnapshot(app core.App, user *core.Record) (accountPasskeyState, passkeyUser, error) {
	_, capability, _ := configuredPasskeyRP()
	state := accountPasskeyState{Entries: []accountPasskeyEntry{}, Limit: passkeyLimit, Capability: capability}
	identity := passkeyUser{record: user, credentials: []webauthn.Credential{}}
	rows := []storedPasskey{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[user_id]], [[rp_id]], [[credential_json]], [[created_at]], [[last_used_at]] FROM {{leafen_passkeys}}
		WHERE [[user_id]] = {:userId} ORDER BY [[id]] LIMIT 6`).Bind(dbx.Params{"userId": user.Id}).All(&rows); err != nil {
		return state, identity, err
	}
	if len(rows) > passkeyLimit {
		return state, identity, errPasskeyLimit
	}
	material := user.Id + "\x00" + user.TokenKey()
	for _, row := range rows {
		credential := webauthn.Credential{}
		if err := json.Unmarshal([]byte(row.CredentialJSON), &credential); err != nil {
			return state, identity, err
		}
		if !bytes.Equal(credential.ID, decodePasskeyID(row.ID)) || len(credential.PublicKey) == 0 {
			return state, identity, errPasskeyInvalid
		}
		if row.RPID == capability.RPID {
			identity.credentials = append(identity.credentials, credential)
		}
		entry := accountPasskeyEntry{ID: row.ID, RPID: row.RPID, CreatedAt: time.UnixMilli(row.CreatedAt).UTC().Format(time.RFC3339Nano), BackedUp: credential.Flags.BackupState}
		if row.LastUsedAt > 0 {
			entry.LastUsedAt = time.UnixMilli(row.LastUsedAt).UTC().Format(time.RFC3339Nano)
		}
		state.Entries = append(state.Entries, entry)
		// Login counter/last-use updates do not invalidate a pending enrollment.
		material += "\x00" + row.ID + "\x00" + row.RPID
	}
	state.Revision = hashAuthMFAValue(material)
	err := authorizeEnterpriseSSOSignIn(app, user.Id, "passkey", "")
	state.SSORequired = errors.Is(err, errSAMLRequired)
	if err != nil && !state.SSORequired {
		return state, identity, err
	}
	state.CanAdd = capability.Available && !state.SSORequired && len(rows) < passkeyLimit
	return state, identity, nil
}

func decodePasskeyID(value string) []byte {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) == 0 || len(decoded) > 1023 {
		return nil
	}
	return decoded
}

// Repeat admission inside the write transaction, closing the middleware /
// password rotation / session revocation race before a key is changed.
func currentPasskeyAccount(event *core.RequestEvent, tx core.App) (*core.Record, string, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return nil, "", errAuthSessionInvalid
	}
	current, err := tx.FindRecordById("users", user.Id)
	if err != nil {
		return nil, "", err
	}
	if subtle.ConstantTimeCompare([]byte(current.TokenKey()), []byte(user.TokenKey())) != 1 {
		return nil, "", errAuthSessionInvalid
	}
	sessionID, err := activeBrowserSessionID(tx, bearerToken(event.Request), user.Id)
	return current, sessionID, err
}

func readAccountPasskeys(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return authSessionInvalidAPIError(event)
	}
	state := accountPasskeyState{}
	err := runReadSnapshot(event.Request.Context(), event.App, func(tx core.App) error {
		current, err := tx.FindRecordById("users", user.Id)
		if err != nil {
			return err
		}
		state, _, err = accountPasskeysSnapshot(tx, current)
		return err
	})
	if err != nil {
		return passkeyAPIError(event, err)
	}
	return event.JSON(http.StatusOK, state)
}

func removeAccountPasskey(event *core.RequestEvent) error {
	input := passkeyRemoveInput{}
	if err := event.BindBody(&input); err != nil || len(decodePasskeyID(input.CredentialID)) == 0 {
		return passkeyAPIError(event, errPasskeyInvalid)
	}
	state := accountPasskeyState{}
	owner := ""
	err := event.App.RunInTransaction(func(tx core.App) error {
		current, _, err := currentPasskeyAccount(event, tx)
		if err != nil {
			return err
		}
		owner = current.Id
		state, _, err = accountPasskeysSnapshot(tx, current)
		if err != nil {
			return err
		}
		if state.Revision != input.BaseRevision {
			return errPasskeyConflict
		}
		result, err := tx.DB().NewQuery(`DELETE FROM {{leafen_passkeys}} WHERE [[id]] = {:id} AND [[user_id]] = {:userId}`).Bind(dbx.Params{"id": input.CredentialID, "userId": current.Id}).Execute()
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return errPasskeyConflict
		}
		state, _, err = accountPasskeysSnapshot(tx, current)
		return err
	})
	if err != nil {
		return passkeyAPIError(event, err)
	}
	noticeAccountSecurityChange(event, owner, securityChangePasskeyRemoved, "")
	return event.JSON(http.StatusOK, state)
}
