package app

import (
	"errors"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"time"
)

// scheduledBackupContents keeps this server's pruning of the contents and
// the replica's adding to them from crossing: a content the replica has
// copied but not yet named in its list must not go.
var scheduledBackupContents sync.Mutex

// pruneScheduledBackups keeps the newest `keep` whole snapshots, clears the
// parts runs left when they stopped, and removes the contents no kept
// snapshot names. It answers how many snapshots stay.
func pruneScheduledBackups(store scheduledBackupStore, root string, keep int, key *scheduledBackupKey, now time.Time) (int, error) {
	snapshotsRoot := scheduledBackupStoreJoin(root, scheduledBackupSnapshotsName)
	entries, err := store.List(snapshotsRoot)
	if err != nil {
		return 0, err
	}
	whole := []string{}
	for _, entry := range entries {
		path := scheduledBackupStoreJoin(snapshotsRoot, entry.Name)
		if strings.HasPrefix(entry.Name, scheduledBackupIncompletePrefix) {
			if !entry.Modified.IsZero() && now.Sub(entry.Modified) > scheduledBackupAbandonedAge {
				_ = store.RemoveAll(path)
			}
			continue
		}
		if entry.Dir && !strings.HasPrefix(entry.Name, ".") && scheduledBackupManifestPresent(store, path) {
			whole = append(whole, entry.Name)
		}
	}
	sort.Strings(whole)
	for len(whole) > keep {
		if err := store.RemoveAll(scheduledBackupStoreJoin(snapshotsRoot, whole[0])); err != nil {
			return 0, err
		}
		whole = whole[1:]
	}
	scheduledBackupContents.Lock()
	defer scheduledBackupContents.Unlock()
	// What the continuous replica names stays as well; a replica that
	// can't be read keeps every content.
	named, err := replicaFileNames(store, root, key)
	if err != nil {
		return len(whole), nil
	}
	for _, name := range whole {
		manifest, err := readScheduledBackupManifestIn(store, scheduledBackupStoreJoin(snapshotsRoot, name), key)
		if err != nil {
			// A snapshot that can't be read, as one sealed under another
			// password, keeps every content.
			return len(whole), nil
		}
		for _, file := range manifest.Files {
			if file.Blob != "" {
				named[file.Blob] = true
			} else {
				named[file.SHA256] = true
			}
		}
	}
	blobs := scheduledBackupStoreJoin(root, scheduledBackupBlobsName, "sha256")
	prefixes, err := store.List(blobs)
	if errors.Is(err, fs.ErrNotExist) {
		return len(whole), nil
	}
	if err != nil {
		return 0, err
	}
	for _, prefix := range prefixes {
		place := scheduledBackupStoreJoin(blobs, prefix.Name)
		if !prefix.Dir {
			if strings.HasPrefix(prefix.Name, scheduledBackupIncomingPrefix) && !prefix.Modified.IsZero() &&
				now.Sub(prefix.Modified) > scheduledBackupAbandonedAge {
				_ = store.RemoveAll(place)
			}
			continue
		}
		contents, err := store.List(place)
		if err != nil {
			return 0, err
		}
		for _, content := range contents {
			if content.Dir {
				continue
			}
			if strings.HasPrefix(content.Name, scheduledBackupIncomingPrefix) {
				if !content.Modified.IsZero() && now.Sub(content.Modified) > scheduledBackupAbandonedAge {
					_ = store.RemoveAll(scheduledBackupStoreJoin(place, content.Name))
				}
				continue
			}
			if sha256Pattern.MatchString(content.Name) && !named[content.Name] {
				if err := store.RemoveAll(scheduledBackupStoreJoin(place, content.Name)); err != nil {
					return 0, err
				}
			}
		}
	}
	return len(whole), nil
}

func scheduledBackupManifestPresent(store scheduledBackupStore, snapshot string) bool {
	for _, name := range []string{scheduledBackupManifestName, scheduledBackupManifestName + ".sealed"} {
		if scheduledBackupStoreExists(store, scheduledBackupStoreJoin(snapshot, name)) {
			return true
		}
	}
	return false
}
