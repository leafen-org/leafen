package organization

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/elimity-com/scim"
	scimErrors "github.com/elimity-com/scim/errors"
	"github.com/pocketbase/pocketbase/core"
)

// findOrCreateSCIMUser finds the account of an address, or makes it with a
// password no one knows — the one hashed before the transaction when given
// (password_outside_transaction.go).
func findOrCreateSCIMUser(app core.App, email string, name string, unknown *leafen.PreparedPassword) (*core.Record, error) {
	user, err := leafen.FindAccountByEmail(app, email)
	if err == nil {
		// The organization proves the address it provisions; an account that
		// held it unproven becomes its owner's (account_email_proof.go).
		if err := leafen.ClaimUnprovenAddress(app, user, email); err != nil {
			return nil, err
		}
		return user, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	collection, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return nil, err
	}
	if unknown == nil {
		password, err := leafen.UnknownPassword(app, "lsp_")
		if err != nil {
			return nil, err
		}
		unknown = &password
	}
	user = core.NewRecord(collection)
	user.SetEmail(email)
	leafen.SetPreparedPassword(*unknown, user)
	user.SetVerified(true)
	user.Set("name", strings.TrimSpace(name))
	if err := app.Save(user); err != nil {
		return nil, err
	}
	if err := leafen.AdmitVerifiedEmailInvitations(app, user); err != nil {
		return nil, err
	}
	return user, nil
}

// scimUnknownPassword hashes, before the transaction, the password no one
// knows of an address that has no account yet; nil when it has one.
func scimUnknownPassword(app core.App, email string) *leafen.PreparedPassword {
	if _, err := leafen.FindAccountByEmail(app, email); !errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	password, err := leafen.UnknownPassword(app, "lsp_")
	if err != nil {
		return nil
	}
	return &password
}

func scimStringAttribute(attributes scim.ResourceAttributes, key string) (string, bool) {
	value, ok := attributes[key].(string)
	return strings.TrimSpace(value), ok && strings.TrimSpace(value) != ""
}

func scimBoolAttribute(attributes scim.ResourceAttributes, key string, fallback bool) bool {
	value, ok := attributes[key].(bool)
	if !ok {
		return fallback
	}
	return value
}

func scimMembershipStatus(active bool) string {
	if active {
		return leafen.OrganizationMemberStatusActive
	}
	return leafen.OrganizationMemberStatusDeactivated
}

func newSCIMRecordID(prefix string) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(random)[:15-len(prefix)], nil
}

func normalizeSCIMStorageError(app core.App, reference string, stage string, err error) error {
	var scimError scimErrors.ScimError
	if errors.As(err, &scimError) {
		return scimError
	}
	app.Logger().Error(
		"leafen SCIM storage failed", "diagnosticReference", reference,
		"code", leafen.ErrorCodeSCIMRequestFailed, "operation", "scim.resource",
		"stage", leafen.SafeDiagnosticToken(stage), "causeClass", fmt.Sprintf("%T", err),
		"sourcePosition", leafen.DiagnosticSourcePosition(1),
	)
	return scimErrors.ScimErrorInternal
}
