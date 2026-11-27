package app

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// The key a migration's reader hands its reading in with
// (notion_migration_formats_read.go). A migration has one key at a time,
// and a key takes one reading: it ends when its reader hands a reading in,
// kept or refused, or two hours after Leafen last gave it out. Until then
// every leafen_notion_migration_formats gives the same key again, so the
// check the guide asks for while the reader runs — has it read yet? — never
// ends the key of the reader still reading in the person's tab (2026-11-18:
// each check made a new key and ended the running reader's, and its reading
// came back refused). Leafen knows a key by its hash, and keeps it sealed
// with the server's secret only to give the same key again.

const (
	notionMigrationFormatsKeyLife = 2 * time.Hour
	// The reader hands in what it has read by this long before its key
	// ends, even while Notion still asks it to wait.
	notionMigrationFormatsKeyMargin = 5 * time.Minute
)

// notionMigrationFormatsKey is the key Leafen gives a migration's reader.
type notionMigrationFormatsKey struct {
	Key string
	// Again says Leafen gave this key before and no reading came in with it.
	Again   bool
	GivenAt time.Time
	Expires time.Time
}

func notionMigrationFormatsKeyHash(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:])
}

// giveNotionMigrationFormatsKey gives the migration's reader key: the one
// given before while no reading came in with it and it hasn't ended,
// otherwise a new one. Either way it holds two hours from now. The reader's
// row comes back as it was kept.
func giveNotionMigrationFormatsKey(app core.App, stored storedNotionMigration) (notionMigrationFormatsKey, notionMigrationFormatsReader, error) {
	given, kept := notionMigrationFormatsKey{}, notionMigrationFormatsReader{}
	cipher, err := notionMigrationFormatsKeyCipher(app)
	if err != nil {
		return given, kept, err
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		reader, _, err := loadNotionMigrationFormatsReader(txApp, stored.ID)
		if err != nil {
			return err
		}
		now := time.Now()
		if reader.KeyHash != "" && reader.KeySealed != "" && reader.Expires > now.UnixMilli() {
			// A key sealed with a secret this server no longer has gives way to a new one.
			if opened, err := security.Decrypt(reader.KeySealed, cipher); err == nil &&
				subtle.ConstantTimeCompare([]byte(notionMigrationFormatsKeyHash(string(opened))), []byte(reader.KeyHash)) == 1 {
				given.Key, given.Again = string(opened), true
			}
		}
		if !given.Again {
			given.Key = security.RandomString(40)
			sealed, err := security.Encrypt([]byte(given.Key), cipher)
			if err != nil {
				return err
			}
			reader.KeyHash, reader.KeySealed, reader.GivenAt = notionMigrationFormatsKeyHash(given.Key), sealed, now.UnixMilli()
		}
		reader.Expires = now.Add(notionMigrationFormatsKeyLife).UnixMilli()
		given.GivenAt, given.Expires = time.UnixMilli(reader.GivenAt), time.UnixMilli(reader.Expires)
		kept = reader
		return storeNotionMigrationFormat(txApp, stored.ID, stored.WorkspaceID, "reader", "", reader)
	})
	return given, kept, err
}

// endNotionMigrationFormatsKey ends the reader's key once a reading came in
// with it, kept or refused; its hash stays to tell a second hand-in of the
// same reader that Leafen has its reading.
func endNotionMigrationFormatsKey(reader *notionMigrationFormatsReader) {
	reader.UsedHash, reader.UsedAt = reader.KeyHash, time.Now().UnixMilli()
	reader.KeyHash, reader.KeySealed, reader.Expires = "", "", 0
}

// notionMigrationFormatsKeyCipher seals reader keys with the server's secret.
func notionMigrationFormatsKeyCipher(app core.App) (string, error) {
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte("leafen-notion-migration-formats-key-v1\x00" + secret))
	return string(digest[:]), nil
}

// notionMigrationFormatsKeyState is what a key handed in with a reading is
// to its migration's reader now.
type notionMigrationFormatsKeyState int

const (
	// Ended: not the reader's key, or past its two hours.
	notionMigrationFormatsKeyEnded notionMigrationFormatsKeyState = iota
	// Used: a reading came in with it already.
	notionMigrationFormatsKeyUsed
	// Live: the reader's key, which takes this reading.
	notionMigrationFormatsKeyLive
)

func notionMigrationFormatsKeyStateOf(reader notionMigrationFormatsReader, hash string) notionMigrationFormatsKeyState {
	switch {
	case reader.KeyHash != "" && subtle.ConstantTimeCompare([]byte(reader.KeyHash), []byte(hash)) == 1:
		if reader.Expires >= time.Now().UnixMilli() {
			return notionMigrationFormatsKeyLive
		}
	case reader.UsedHash != "" && subtle.ConstantTimeCompare([]byte(reader.UsedHash), []byte(hash)) == 1:
		return notionMigrationFormatsKeyUsed
	}
	return notionMigrationFormatsKeyEnded
}
