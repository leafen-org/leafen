package app

import "github.com/pocketbase/pocketbase/core"

// Hashing a password or checking one (bcrypt, about 65ms) in a write
// transaction holds the server's only write connection, so every save waits
// behind sign-ins and sign-ups (measured 2026-10-22: saves at 4s p50 while 64
// sign-ins ran at a time). Both are done before the transaction: inside, a
// hash made before is set, and a password checked before stands while the
// account's stored hash is still the one checked — changed since, it is
// checked again there, so an old password never gets in.

// preparedPassword is a password hashed before the transaction that sets it.
type preparedPassword struct {
	plain string
	value any
}

// preparePassword hashes a password for an account of a collection, as
// PocketBase does when it is set, with that collection's cost.
func preparePassword(app core.App, collection, plain string) preparedPassword {
	prepared := preparedPassword{plain: plain}
	if plain == "" {
		return prepared
	}
	if accounts, err := app.FindCollectionByNameOrId(collection); err == nil {
		scratch := core.NewRecord(accounts)
		scratch.SetPassword(plain)
		prepared.value = scratch.GetRaw(core.FieldNamePassword)
	}
	return prepared
}

// set puts the password on an account: as hashed before, else hashed now.
func (prepared preparedPassword) set(record *core.Record) {
	if prepared.value != nil {
		record.SetRaw(core.FieldNamePassword, prepared.value)
		return
	}
	record.SetPassword(prepared.plain)
}

// unknownPassword is a password no one knows, for an account that signs in
// otherwise, hashed before the transaction that makes the account.
func unknownPassword(app core.App, prefix string) (preparedPassword, error) {
	password, err := newAuthProviderToken(prefix, 32)
	if err != nil {
		return preparedPassword{}, err
	}
	return preparePassword(app, "users", password), nil
}

// passwordCheck is a password checked against an account before the
// transaction; the zero check has nothing checked.
type passwordCheck struct {
	hash  string
	valid bool
}

func checkPassword(record *core.Record, plain string) passwordCheck {
	return passwordCheck{hash: record.GetString(core.FieldNamePassword + ":hash"), valid: record.ValidatePassword(plain)}
}

// holds says whether a password is the account's as read in the transaction:
// the earlier answer while its stored hash is unchanged, else checked now.
func (check passwordCheck) holds(record *core.Record, plain string) bool {
	if check.hash != "" && record.GetString(core.FieldNamePassword+":hash") == check.hash {
		return check.valid
	}
	return record.ValidatePassword(plain)
}
