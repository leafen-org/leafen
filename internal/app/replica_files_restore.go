package app

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
)

// What restoring and pruning read of the replica's file list
// (replica_files.go).

// replicaFileNames are the contents the replica's list names, for the
// snapshots' pruning to keep.
func replicaFileNames(store scheduledBackupStore, root string, key *scheduledBackupKey) (map[string]bool, error) {
	held, err := readReplicaFileList(store, root, key)
	if err != nil {
		return nil, err
	}
	named := map[string]bool{}
	for _, entry := range held {
		if entry.Blob != "" {
			named[entry.Blob] = true
		} else {
			named[entry.SHA256] = true
		}
	}
	return named, nil
}

// assembleReplicaFiles writes the replica's files into a data folder being
// put together, each checked against its SHA-256.
func assembleReplicaFiles(store scheduledBackupStore, root string, key *scheduledBackupKey, target string, progress *backupProgress) (int, int64, error) {
	held, err := readReplicaFileList(store, root, key)
	if err != nil {
		return 0, 0, err
	}
	var bytes, total int64
	for _, entry := range held {
		total += entry.Bytes
	}
	progress.step(backupProgressStepFiles, total, len(held))
	for _, entry := range held {
		content, err := key.reader(store, scheduledBackupBlobStorePath(root, key.blobName(entry.SHA256)))
		if err != nil {
			return 0, 0, errors.Join(errors.New("the replica's content for "+entry.Path+" is missing"), err)
		}
		path := filepath.Join(target, filepath.FromSlash(entry.Path))
		err = errors.Join(os.MkdirAll(filepath.Dir(path), 0o700), restoreScheduledBackupFile(progress.reader(content), path, entry))
		content.Close()
		if err != nil {
			return 0, 0, err
		}
		progress.done(0, 1)
		bytes += entry.Bytes
	}
	return len(held), bytes, nil
}

// keepBackupFolderAfterRestore gives a restored database the backup folder
// this server reached the replica at, where the server that wrote it named
// another: a folder may be mounted at another path on each machine. Places
// with an address (WebDAV, S3) are the same everywhere and stay as they
// are, with their keys.
func keepBackupFolderAfterRestore(database, folder string) error {
	if folder == "" {
		return nil
	}
	db, err := sql.Open("sqlite", "file:"+database+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE leafen_scheduled_backups SET folder = ? WHERE id = 'default' AND target = ? AND folder <> ?`,
		folder, scheduledBackupTargetFolder, folder)
	return errors.Join(err, db.Close())
}
