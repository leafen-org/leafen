package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Whose replica it is (replica_runner.go): the place's "owner.json", this
// server's mark and the place it last held, the note that its data moved
// elsewhere, and how a place is named.

// errReplicaMoved says another server took the replica over.
type errReplicaMoved struct{ origin string }

func (moved errReplicaMoved) Error() string { return "another server took the replica over" }

// replicaOwner is the replica's "owner.json".
type replicaOwner struct {
	Holder string `json:"holder"`
	// Origin is the owner's address, for a server it replaced to point to.
	Origin string `json:"origin,omitempty"`
	// KeyID is the backup key the replica is sealed with, "" for none: a
	// replica sealed another way starts again.
	KeyID string `json:"keyId,omitempty"`
	Since int64  `json:"since"`
}

// replicaHolder is this server's mark, and the place whose replica it last
// held (named as replicaPlace names it).
type replicaHolder struct {
	Holder  string `json:"holder"`
	Claimed string `json:"claimed,omitempty"`
	// Paired is whether a standby followed this server when it last looked:
	// a paired primary that can't raise its beat pauses.
	Paired bool `json:"paired,omitempty"`
	// PairSecret signs the pair's direct checks (replica_pair_check.go): a
	// primary's own, which its connection code carries, or the primary's,
	// on its standby. Only this mark holds it; no backup or replica does.
	PairSecret string `json:"pairSecret,omitempty"`
	// JoinToken is a primary's newest connection code's: a standby joins
	// only with it, so making a code again closes the codes before it that
	// nobody used (2026-11-05: a code copied where it shouldn't have been
	// couldn't be taken back). A standby already paired goes on by the pair
	// secret, which stays.
	JoinToken string `json:"joinToken,omitempty"`
}

// replicaFence is what a moved server keeps: where its data went.
type replicaFence struct {
	MovedTo string `json:"movedTo,omitempty"`
	At      int64  `json:"at"`
}

func replicaRoot() string {
	return scheduledBackupStoreJoin(scheduledBackupRootName, replicaFolderName)
}

func readReplicaOwner(store scheduledBackupStore) (*replicaOwner, error) {
	raw, err := scheduledBackupStoreReadAll(store, scheduledBackupStoreJoin(replicaRoot(), replicaOwnerName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	owner := &replicaOwner{}
	if err := json.Unmarshal(raw, owner); err != nil || owner.Holder == "" {
		return nil, errors.New("the replica's owner.json is not valid")
	}
	return owner, nil
}

func writeReplicaOwner(store scheduledBackupStore, owner replicaOwner) error {
	raw, err := json.MarshalIndent(owner, "", " ")
	if err != nil {
		return err
	}
	if err := store.MakeDir(replicaRoot()); err != nil {
		return err
	}
	return store.Replace(scheduledBackupStoreJoin(replicaRoot(), replicaOwnerName), raw)
}

// loadReplicaHolder reads this server's mark, making it the first time.
func loadReplicaHolder(dataDir string) (replicaHolder, error) {
	holder := replicaHolder{}
	raw, err := os.ReadFile(filepath.Join(dataDir, replicaHolderName))
	if err == nil && json.Unmarshal(raw, &holder) == nil && holder.Holder != "" {
		return holder, nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return holder, err
	}
	mark := make([]byte, 16)
	if _, err := rand.Read(mark); err != nil {
		return holder, err
	}
	holder = replicaHolder{Holder: hex.EncodeToString(mark)}
	return holder, saveReplicaHolder(dataDir, holder)
}

func saveReplicaHolder(dataDir string, holder replicaHolder) error {
	raw, err := json.Marshal(holder)
	if err != nil {
		return err
	}
	return writeRestoreFile(dataDir, replicaHolderName, json.RawMessage(raw))
}

func readReplicaFence(dataDir string) *replicaFence {
	fence := &replicaFence{}
	if readRestoreFile(filepath.Join(dataDir, replicaFencedName), fence) != nil {
		return nil
	}
	return fence
}

// replicaPlace names a place, as this server keeps whose replica it held.
func replicaPlace(stored storedScheduledBackup) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{stored.Target, stored.Folder, stored.WebDAVURL, stored.WebDAVUsername,
		stored.S3Endpoint, stored.S3Bucket, stored.S3Prefix}, "\x00")))
	return hex.EncodeToString(digest[:12])
}

// replicaDestination names a place and how its replica is sealed, kept only
// in memory: a change in either starts the replica again.
func replicaDestination(app core.App, stored storedScheduledBackup) string {
	secret, _ := scheduledBackupSecretOf(app, stored)
	identity := stored.PasswordCipher + "\x00" + secret.Password
	if secret.Record != nil {
		identity = "key\x00" + secret.Record.ID
	}
	sealed := sha256.Sum256([]byte(identity))
	return replicaPlace(stored) + "-" + hex.EncodeToString(sealed[:8])
}
