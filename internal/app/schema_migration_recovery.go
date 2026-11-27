package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

// The recovery point a start keeps before it applies new migrations to data
// that has some (runProductMigrations), once for each pair of versions: in
// backups/migrations/, leafen_pre_migration_v<from>_to_v<to>/, a data folder
// as the data was (schema_migration_recovery_folder.go), and its receipt
// beside it, <same name>.json. To go back, that folder is copied as the data
// folder of the release from before. Until 2026-11-22 the recovery point was a
// whole backup zip of the data folder: on a NAS with 4.0 GB of data, 3.1 GB of
// it attachments, the zip took the server over three minutes before it
// listened, and the update's script took it for a server that didn't start.
// A zip an earlier build made for the same versions still stands. Points
// the data has gone past go once a later start has served a while
// (schema_migration_recovery_prune.go).

type migrationRecoveryReceipt struct {
	Status      string `json:"status"`
	FromVersion int    `json:"fromVersion"`
	ToVersion   int    `json:"toVersion"`
	// Archive names the zip of the earlier form; Folder the data folder.
	Archive string `json:"archive,omitempty"`
	Folder  string `json:"folder,omitempty"`
	// Bytes and SHA256 are the zip's, or the folder's data.db's.
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	// Files are the folder's other files: Cloned, Linked or Copied.
	Files     int    `json:"files,omitempty"`
	FileBytes int64  `json:"fileBytes,omitzero"`
	Cloned    int    `json:"cloned,omitzero"`
	Linked    int    `json:"linked,omitempty"`
	Copied    int    `json:"copied,omitempty"`
	CreatedAt string `json:"createdAt"`
}

func migrationRecoveryName(fromVersion int, toVersion int) string {
	return fmt.Sprintf("leafen_pre_migration_v%03d_to_v%03d", fromVersion, toVersion)
}

// migrationRecoveryPointName is a pair's recovery point on disk: name is the
// pair's name, or with -2, -3 … for one kept again for the same pair, and
// zip says it is a zip of the earlier form.
type migrationRecoveryPointName struct {
	name string
	zip  bool
}

// migrationRecoveryNameAt is the pair's nth point's name, the first without
// a number.
func migrationRecoveryNameAt(base string, n int) string {
	if n == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, n)
}

// migrationRecoveryNumbers are the numbers of the pair's names in root, with
// whether each has its point (a folder or a zip) and its receipt.
func migrationRecoveryNumbers(root string, base string) (map[int][2]bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	numbers := map[int][2]bool{}
	for _, entry := range entries {
		rest, ok := strings.CutPrefix(entry.Name(), base)
		if !ok {
			continue
		}
		receipt := strings.HasSuffix(rest, ".json")
		rest = strings.TrimSuffix(strings.TrimSuffix(rest, ".json"), ".zip")
		n := 1
		if rest != "" {
			number, err := strconv.Atoi(strings.TrimPrefix(rest, "-"))
			if !strings.HasPrefix(rest, "-") || err != nil || number < 2 {
				continue
			}
			n = number
		}
		seen := numbers[n]
		if receipt {
			seen[1] = true
		} else {
			seen[0] = true
		}
		numbers[n] = seen
	}
	return numbers, nil
}

// newestMigrationRecoveryPoint is the pair's latest point, its name "" when
// it has none; a receipt later than it with no point stops the start, as
// one never made whole.
func newestMigrationRecoveryPoint(root string, base string) (migrationRecoveryPointName, error) {
	numbers, err := migrationRecoveryNumbers(root, base)
	if err != nil {
		return migrationRecoveryPointName{}, err
	}
	newest := 0
	for n, seen := range numbers {
		if seen[0] && n > newest {
			newest = n
		}
	}
	for n, seen := range numbers {
		if n > newest && seen[1] {
			return migrationRecoveryPointName{}, errors.New("migration recovery receipt has no recovery point")
		}
	}
	if newest == 0 {
		return migrationRecoveryPointName{}, nil
	}
	name := migrationRecoveryNameAt(base, newest)
	folder, err := directoryExists(filepath.Join(root, name))
	if err != nil {
		return migrationRecoveryPointName{}, err
	}
	return migrationRecoveryPointName{name: name, zip: !folder}, nil
}

// nextMigrationRecoveryName is the pair's name after every one taken.
func nextMigrationRecoveryName(root string, base string) (string, error) {
	numbers, err := migrationRecoveryNumbers(root, base)
	if err != nil {
		return "", err
	}
	next := 1
	for n := range numbers {
		next = max(next, n+1)
	}
	return migrationRecoveryNameAt(base, next), nil
}

// settleMigrationRecoveryPoint holds a point to its receipt, writing the
// receipt where a start stopped before it, and gives the receipt: the folder
// takes its name only once whole and durable, so one that's there is whole.
func settleMigrationRecoveryPoint(root string, point migrationRecoveryPointName, fromVersion int, toVersion int) (migrationRecoveryReceipt, error) {
	expected := migrationRecoveryReceipt{Status: "created", FromVersion: fromVersion, ToVersion: toVersion}
	var digest string
	var bytes int64
	var err error
	if point.zip {
		archivePath := filepath.Join(root, point.name+".zip")
		if err := validateBackupArchive(archivePath); err != nil {
			return expected, err
		}
		expected.Archive = point.name + ".zip"
		digest, bytes, err = fileSHA256(archivePath)
	} else {
		expected.Folder = point.name
		digest, bytes, err = fileSHA256(filepath.Join(root, point.name, dataDatabaseFile))
	}
	if err != nil {
		return expected, err
	}
	expected.Bytes, expected.SHA256 = bytes, digest
	receiptPath := filepath.Join(root, point.name+".json")
	exists, err := regularFileExists(receiptPath)
	if err != nil {
		return expected, err
	}
	if exists {
		return expected, validateMigrationRecoveryReceipt(receiptPath, expected)
	}
	expected.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return expected, writeMigrationRecoveryReceipt(receiptPath, expected)
}

// ensureMigrationRecovery keeps the data as it is before migrations from
// fromVersion to toVersion apply. A point kept for the same pair before — an
// attempt that failed — stands while the data is still as it holds it (a
// start tried again at once, a restart loop); once the data changed (the
// release from before was used meanwhile, 2026-11-22), it is kept again
// beside it, so going back gives the data as it was right before this
// attempt, and the earlier point goes by the keep rule
// (schema_migration_recovery_prune.go). Whether it changed is told by the
// database's copy, page for page the same while nothing was written: a
// zip of the earlier form can't be told so, and is kept again too.
func ensureMigrationRecovery(pb *pocketbase.PocketBase, fromVersion int, toVersion int) error {
	recoveryRoot := filepath.Join(pb.DataDir(), core.LocalBackupsDirName, "migrations")
	base := migrationRecoveryName(fromVersion, toVersion)
	if err := os.MkdirAll(recoveryRoot, 0o700); err != nil {
		return err
	}
	earlier, err := newestMigrationRecoveryPoint(recoveryRoot, base)
	if err != nil {
		return err
	}
	var earlierReceipt migrationRecoveryReceipt
	if earlier.name != "" {
		if earlierReceipt, err = settleMigrationRecoveryPoint(recoveryRoot, earlier, fromVersion, toVersion); err != nil {
			return err
		}
	}

	// It is made in a folder of its own in Leafen's own work folder in the
	// data folder, which backups leave out and which is on the data folder's
	// disk, then renamed into the backups folder: two servers with folders
	// side by side start at once after an update (2026-11-05), the data
	// folder's parent may be a place the server can't write, "/" for the
	// image's own --dir=/data (2026-11-12), or another disk than a data folder
	// mounted on its own — and a clone or a link goes only within one disk.
	work := leafenTempDir(pb.DataDir())
	if err := os.MkdirAll(work, 0o700); err != nil {
		return err
	}
	temporaryFolder, err := os.MkdirTemp(work, "migration-recovery-")
	if err != nil {
		return err
	}
	// What it made goes; a linked file stays in the data folder.
	defer os.RemoveAll(temporaryFolder)
	staged := filepath.Join(temporaryFolder, "point")
	started := time.Now()
	if earlier.name == "" {
		fmt.Fprintf(os.Stderr, "leafen: keeping the data as it is at schema %d before bringing it up to %d, in %s\n",
			fromVersion, toVersion, filepath.Join(recoveryRoot, base))
	}
	kept, err := stageMigrationRecoveryDatabase(pb, staged)
	if err != nil {
		return err
	}
	if earlier.name != "" && !earlier.zip && kept.DatabaseSHA256 == earlierReceipt.SHA256 {
		fmt.Fprintf(os.Stderr, "leafen: the data is as %s holds it, which stays the way back\n", filepath.Join(recoveryRoot, earlier.name))
		return nil
	}
	name, err := nextMigrationRecoveryName(recoveryRoot, base)
	if err != nil {
		return err
	}
	if earlier.name != "" {
		fmt.Fprintf(os.Stderr, "leafen: the data changed since %s was kept; keeping it as it is now in %s\n",
			filepath.Join(recoveryRoot, earlier.name), filepath.Join(recoveryRoot, name))
	}
	if err := keepMigrationRecoveryFiles(pb, staged, &kept); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "leafen: kept in %.1f s: the database, %d MB, in %.1f s; %d files, %d cloned, %d linked, %d copied\n",
		time.Since(started).Seconds(), kept.DatabaseBytes>>20, kept.DatabaseTook.Seconds(), kept.Files, kept.Cloned, kept.Linked, kept.Copied)
	if err := os.Rename(staged, filepath.Join(recoveryRoot, name)); err != nil {
		return err
	}
	if err := syncDirectory(recoveryRoot); err != nil {
		return err
	}
	return writeMigrationRecoveryReceipt(filepath.Join(recoveryRoot, name+".json"), migrationRecoveryReceipt{
		Status: "created", FromVersion: fromVersion, ToVersion: toVersion, Folder: name,
		Bytes: kept.DatabaseBytes, SHA256: kept.DatabaseSHA256,
		Files: kept.Files, FileBytes: kept.FileBytes, Cloned: kept.Cloned, Linked: kept.Linked, Copied: kept.Copied,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// validateMigrationRecoveryReceipt holds a receipt to what its recovery point
// is now; what it counts besides is said, not checked.
func validateMigrationRecoveryReceipt(path string, expected migrationRecoveryReceipt) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var receipt migrationRecoveryReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return err
	}
	if receipt.Status != expected.Status || receipt.Archive != expected.Archive || receipt.Folder != expected.Folder ||
		receipt.FromVersion != expected.FromVersion || receipt.ToVersion != expected.ToVersion ||
		receipt.Bytes != expected.Bytes || receipt.SHA256 != expected.SHA256 || receipt.CreatedAt == "" {
		return errors.New("migration recovery receipt does not match its recovery point")
	}
	return nil
}

func writeMigrationRecoveryReceipt(path string, receipt migrationRecoveryReceipt) error {
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".leafen-migration-receipt-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := errors.Join(temporary.Sync(), temporary.Close()); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func directoryExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("recovery path is not a folder: %s", filepath.Base(path))
	}
	return true, nil
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("recovery path is not a regular file: %s", filepath.Base(path))
	}
	return true, nil
}
