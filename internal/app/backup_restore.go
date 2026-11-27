package app

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/tools/archive"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/spf13/cobra"
)

const (
	backupManifestPath     = "leafen/backup-manifest.json"
	backupFormat           = "leafen.backup"
	backupFormatVersion    = 1
	maxBackupManifestBytes = 4096
	errorCodeBackupFailed  = "LEAFEN_BACKUP_FAILED"
	errorCodeRestoreFailed = "LEAFEN_RESTORE_FAILED"
)

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type backupReceipt struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type restoreReceipt struct {
	Status string `json:"status"`
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type backupManifest struct {
	Format        string `json:"format"`
	FormatVersion int    `json:"formatVersion"`
}

func backupCommand(pb *pocketbase.PocketBase) *cobra.Command {
	var output string
	before := &restoreTargetBefore{}
	command := &cobra.Command{
		Use:          "backup",
		Short:        "Create a complete Leafen backup",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			// A folder that held no data.db before the start is no server's:
			// the start made an empty one there, and its backup read as made
			// and restored as a first setup (2026-11-03 audit, --dir given an
			// empty folder or the data folder's parent). It is turned away,
			// and what the start made goes — all of it, even where the folder
			// held other Leafen entries (a backup's staging beside its zip).
			if before.noted && !before.entries["data.db"] {
				refusal := fmt.Errorf("no Leafen data in %s: give --dir the server's data folder, the one holding data.db", pb.DataDir())
				made := *before
				made.leafen = false
				return fmt.Errorf("%s: %w", errorCodeBackupFailed, errors.Join(refusal, removeRefusedRestoreTraces(pb, &made, refusal)))
			}
			receipt, err := createExternalBackup(command.Context(), pb, output)
			if err != nil {
				return fmt.Errorf("%s: %w", errorCodeBackupFailed, err)
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(receipt)
		},
	}
	noteRestoreTargetBeforeStart(pb, command, before)
	command.Flags().StringVar(&output, "output", "", "external backup zip path")
	_ = command.MarkFlagRequired("output")
	return command
}

func restoreCommand(pb *pocketbase.PocketBase) *cobra.Command {
	var input string
	var expectedSHA256 string
	var passwordFile string
	var codeFile string
	var replica bool
	before := &restoreTargetBefore{}
	command := &cobra.Command{
		Use:          "restore",
		Short:        "Restore a complete Leafen backup into a fresh data root",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			// A sealed backup's recovery code or password: a file's first
			// line, or the environment's, never an argument others could read.
			firstLine := func(path, fallback string) (string, error) {
				if path == "" {
					return fallback, nil
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					return "", err
				}
				return strings.TrimRight(strings.SplitN(string(raw), "\n", 2)[0], "\r"), nil
			}
			password, err := firstLine(passwordFile, os.Getenv(scheduledBackupPasswordEnv))
			if err != nil {
				return fmt.Errorf("%s: %w", errorCodeRestoreFailed, err)
			}
			code, err := firstLine(codeFile, os.Getenv(scheduledBackupRecoveryCodeEnv))
			if err != nil {
				return fmt.Errorf("%s: %w", errorCodeRestoreFailed, err)
			}
			secret := scheduledBackupSecret{Password: password, Code: code}
			var receipt restoreReceipt
			switch {
			case replica:
				receipt, err = restoreReplicaIntoFreshRoot(command.Context(), pb, input, secret)
			case strings.TrimSpace(input) == "":
				err = errors.New("input path is required")
			default:
				receipt, err = restoreExternalBackup(command.Context(), pb, input, expectedSHA256, secret)
			}
			if err != nil {
				// A refused restore leaves its target as it found it.
				return fmt.Errorf("%s: %w", errorCodeRestoreFailed, errors.Join(err, removeRefusedRestoreTraces(pb, before, err)))
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(receipt)
		},
	}
	noteRestoreTargetBeforeStart(pb, command, before)
	command.Flags().StringVar(&input, "input", "", "external Leafen backup zip path, or a folder snapshot")
	command.Flags().StringVar(&expectedSHA256, "sha256", "",
		"expected backup SHA-256 receipt (required for a zip; for a folder snapshot, its snapshot.json's)")
	command.Flags().StringVar(&passwordFile, "password-file", "",
		"a file whose first line is an encrypted backup's password (or set "+scheduledBackupPasswordEnv+")")
	command.Flags().StringVar(&codeFile, "recovery-code-file", "",
		"a file whose first line is an encrypted backup's recovery code (or set "+scheduledBackupRecoveryCodeEnv+")")
	command.Flags().BoolVar(&replica, "replica", false,
		"restore the continuous replica: from the \""+scheduledBackupRootName+"\" folder given as --input, or from the place the backup environment names")
	return command
}

func createExternalBackup(ctx context.Context, pb *pocketbase.PocketBase, output string) (backupReceipt, error) {
	outputPath, err := filepath.Abs(strings.TrimSpace(output))
	if err != nil || strings.TrimSpace(output) == "" {
		return backupReceipt{}, errors.New("output path is required")
	}
	if pathWithin(pb.DataDir(), outputPath) {
		return backupReceipt{}, errors.New("output path must be outside the Leafen data root")
	}
	return writeLeafenBackup(ctx, pb, outputPath)
}

// writeLeafenBackup writes the backup at a path outside the data folder that
// isn't there yet.
func writeLeafenBackup(ctx context.Context, pb *pocketbase.PocketBase, output string) (backupReceipt, error) {
	outputPath, err := filepath.Abs(output)
	if err != nil {
		return backupReceipt{}, err
	}
	if pathWithin(pb.DataDir(), outputPath) {
		return backupReceipt{}, errors.New("output path must be outside the Leafen data root")
	}
	if _, err := os.Stat(outputPath); err == nil {
		return backupReceipt{}, errors.New("output path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return backupReceipt{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return backupReceipt{}, err
	}
	destinationLease, err := acquireBackupDestinationLease(filepath.Dir(outputPath))
	if err != nil {
		return backupReceipt{}, err
	}
	defer destinationLease.Close()

	name := "leafen_cli_" + security.RandomString(16) + ".zip"
	if err := createBackupUnderLease(ctx, pb, name); err != nil {
		return backupReceipt{}, err
	}
	backups, err := pb.NewBackupsFilesystem()
	if err != nil {
		return backupReceipt{}, err
	}
	defer backups.Close()
	defer backups.Delete(name)
	backups.SetContext(ctx)
	reader, err := backups.GetReader(name)
	if err != nil {
		return backupReceipt{}, err
	}
	defer reader.Close()

	rawTemporary, err := os.CreateTemp(destinationLease.stagingRoot, backupRawTemporaryPattern)
	if err != nil {
		return backupReceipt{}, err
	}
	rawTemporaryPath := rawTemporary.Name()
	defer os.Remove(rawTemporaryPath)
	_, copyErr := io.Copy(rawTemporary, reader)
	closeErr := errors.Join(rawTemporary.Sync(), rawTemporary.Close())
	if err := errors.Join(copyErr, closeErr); err != nil {
		return backupReceipt{}, err
	}

	versionedTemporary, err := os.CreateTemp(destinationLease.stagingRoot, backupVersionedTemporaryPattern)
	if err != nil {
		return backupReceipt{}, err
	}
	versionedTemporaryPath := versionedTemporary.Name()
	defer os.Remove(versionedTemporaryPath)
	writeErr := writeVersionedBackupArchive(rawTemporaryPath, versionedTemporary)
	closeErr = errors.Join(versionedTemporary.Sync(), versionedTemporary.Close())
	if err := errors.Join(writeErr, closeErr); err != nil {
		return backupReceipt{}, err
	}
	backupSHA256, bytesWritten, err := fileSHA256(versionedTemporaryPath)
	if err != nil {
		return backupReceipt{}, err
	}
	if _, err := os.Stat(outputPath); err == nil {
		return backupReceipt{}, errors.New("output path was created during backup")
	} else if !errors.Is(err, os.ErrNotExist) {
		return backupReceipt{}, err
	}
	if err := publishNoReplace(versionedTemporaryPath, outputPath); err != nil {
		return backupReceipt{}, err
	}
	if err := syncDirectory(filepath.Dir(outputPath)); err != nil {
		return backupReceipt{}, err
	}
	return backupReceipt{
		Status: "created", Path: outputPath, Bytes: bytesWritten,
		SHA256: backupSHA256,
	}, nil
}

func writeVersionedBackupArchive(rawPath string, output io.Writer) error {
	reader, err := zip.OpenReader(rawPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	writer := zip.NewWriter(output)
	manifestHeader := &zip.FileHeader{Name: backupManifestPath, Method: zip.Store}
	manifestWriter, writeErr := writer.CreateHeader(manifestHeader)
	if writeErr == nil {
		var manifest []byte
		manifest, writeErr = json.Marshal(backupManifest{
			Format: backupFormat, FormatVersion: backupFormatVersion,
		})
		if writeErr == nil {
			_, writeErr = manifestWriter.Write(manifest)
		}
	}
	dataDBCount := 0
	for _, file := range reader.File {
		if writeErr != nil {
			break
		}
		if err := validateBackupArchiveEntry(file); err != nil {
			writeErr = err
			break
		}
		if filepath.ToSlash(file.Name) == backupManifestPath {
			writeErr = errors.New("source backup contains the reserved manifest path")
			break
		}
		if filepath.ToSlash(file.Name) == "data.db" {
			dataDBCount++
		}
		writeErr = writer.Copy(file)
	}
	if writeErr == nil && dataDBCount != 1 {
		writeErr = errors.New("source backup must contain one root data.db")
	}
	return errors.Join(writeErr, writer.Close())
}

func restoreExternalBackup(_ context.Context, pb *pocketbase.PocketBase, input, expectedSHA256 string, secret scheduledBackupSecret) (restoreReceipt, error) {
	inputPath, err := filepath.Abs(strings.TrimSpace(input))
	if err != nil || strings.TrimSpace(input) == "" {
		return restoreReceipt{}, errors.New("input path is required")
	}
	expectedSHA256 = strings.ToLower(strings.TrimSpace(expectedSHA256))
	if info, err := os.Stat(inputPath); err == nil && info.IsDir() {
		return restoreScheduledBackupSnapshot(pb, inputPath, expectedSHA256, secret)
	}
	if !sha256Pattern.MatchString(expectedSHA256) {
		return restoreReceipt{}, errors.New("a valid backup SHA-256 receipt is required")
	}
	if pathWithin(pb.DataDir(), inputPath) {
		return restoreReceipt{}, errors.New("input path must be outside the Leafen data root")
	}
	actualSHA256, bytesRead, err := fileSHA256(inputPath)
	if err != nil {
		return restoreReceipt{}, err
	}
	if actualSHA256 != expectedSHA256 {
		return restoreReceipt{}, fmt.Errorf("backup SHA-256 receipt does not match: the file's is %s; give --sha256 the one its backup printed, or copy the file again whole", actualSHA256)
	}
	if err := validateBackupArchive(inputPath); err != nil {
		return restoreReceipt{}, err
	}
	if err := requireFreshRestoreTarget(pb); err != nil {
		return restoreReceipt{}, err
	}
	dataRoot, err := restoreDataRoot(pb, func(extractedRoot string) error {
		if err := archive.Extract(inputPath, extractedRoot); err != nil {
			return err
		}
		manifestOutput := filepath.Join(extractedRoot, filepath.FromSlash(backupManifestPath))
		if err := os.Remove(manifestOutput); err != nil {
			return err
		}
		manifestDirectory := filepath.Dir(manifestOutput)
		manifestSiblings, err := os.ReadDir(manifestDirectory)
		if err != nil {
			return err
		}
		if len(manifestSiblings) == 0 {
			if err := os.Remove(manifestDirectory); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return restoreReceipt{}, err
	}
	return restoreReceipt{Status: "restored", Path: dataRoot, Bytes: bytesRead, SHA256: actualSHA256}, nil
}

// restoreDataRoot fills a folder with fill, then puts its content in the
// data folder's place; what the data folder held, if that can't settle,
// stays as the recovery point. Both folders are in Leafen's own work folder
// in the data folder, which stays put: the data folder's parent may be a
// place the server can't write, "/" for the image's own --dir=/data
// (2026-11-12), or another disk than a data folder mounted on its own, where
// nothing renames across. The backups folder moves with the data, as it
// carries the restored data's note to take a new epoch. A restore goes only
// into a fresh folder, so the recovery point holds what its start made, and
// the work folder clears it a day on as it does what other work left.
func restoreDataRoot(pb *pocketbase.PocketBase, fill func(extractedRoot string) error) (string, error) {
	dataRoot, err := filepath.Abs(pb.DataDir())
	if err != nil {
		return "", err
	}
	parent := leafenTempDir(dataRoot)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	extractedRoot, err := os.MkdirTemp(parent, "restore-extracted-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(extractedRoot)
	oldRoot, err := os.MkdirTemp(parent, "restore-old-")
	if err != nil {
		return "", err
	}
	discardOldRoot := false
	defer func() {
		if discardOldRoot {
			_ = os.RemoveAll(oldRoot)
		}
	}()
	if err := fill(extractedRoot); err != nil {
		discardOldRoot = true
		return "", err
	}
	if err := checkRestoredDataRoot(extractedRoot); err != nil {
		discardOldRoot = true
		return "", err
	}
	// Its first start gives it a new epoch (data_epoch.go); its server's addresses it takes then (restore_server_facts.go).
	if err := errors.Join(markRestoredDataRootForNewEpoch(extractedRoot), dropServerAddresses(extractedRoot)); err != nil {
		discardOldRoot = true
		return "", err
	}
	if err := pb.ClearBootstrap(); err != nil {
		discardOldRoot = true
		return "", err
	}
	if err := publishRestoredDataRoot(dataRoot, extractedRoot, oldRoot, syncPath); err != nil {
		settled, inspectErr := restorePublicationRecoverySettled(oldRoot)
		discardOldRoot = inspectErr == nil && settled
		if !discardOldRoot {
			return "", restorePublicationError{errors.Join(
				err,
				inspectErr,
				fmt.Errorf("restore recovery point retained at %s", oldRoot),
			)}
		}
		return "", err // settled back: refused as any other (restore_refusal_traces.go)
	}
	discardOldRoot = true
	return dataRoot, nil
}

func requireFreshRestoreTarget(pb *pocketbase.PocketBase) error {
	var count int
	err := pb.DB().NewQuery(`
		SELECT
			(SELECT COUNT(*) FROM {{_superusers}}) +
			(SELECT COUNT(*) FROM {{users}}) +
			(SELECT COUNT(*) FROM {{leafen_workspaces}}) +
			(SELECT COUNT(*) FROM {{leafen_pages}}) +
			(SELECT COUNT(*) FROM {{leafen_files}}) +
			(SELECT COUNT(*) FROM {{leafen_ygo_documents}}) +
			(SELECT COUNT(*) FROM {{leafen_ygo_updates}}) +
			(SELECT COUNT(*) FROM {{leafen_ygo_structural_documents}}) +
			(SELECT COUNT(*) FROM {{leafen_ygo_structural_updates}}) +
			(SELECT COUNT(*) FROM {{leafen_ygo_structural_quarantine}}) +
			(SELECT COUNT(*) FROM {{leafen_jobs}}) +
			(SELECT COUNT(*) FROM {{leafen_job_effects}})
	`).Row(&count)
	if err != nil {
		return err
	}
	if count != 0 {
		return errors.New("restore target is not fresh: it already holds Leafen data; restore into an empty folder (this one is left as it is)")
	}
	// A fresh database says nothing about files beside it: the target folder
	// may hold the operator's own files that the restore would move away and
	// then discard with the old root. Refuse before anything moves when the
	// folder holds an entry Leafen did not make.
	if foreign, err := restoreTargetForeignEntry(pb.DataDir()); err != nil {
		return err
	} else if foreign != "" {
		return fmt.Errorf("restore target is not fresh: it holds %q, which Leafen did not make; restore into an empty folder", foreign)
	}
	return nil
}

// restoreTargetForeignEntry names the first top-level entry in the data folder
// that Leafen did not create, or "" when the folder holds only Leafen's own.
// A fresh data root holds the two databases (with their -wal/-shm/-journal
// sidecars), the .notify and .secrets folders, the storage folder, and the
// temporary folders a restore makes.
func restoreTargetForeignEntry(dataRoot string) (string, error) {
	entries, err := os.ReadDir(dataRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	for _, entry := range entries {
		if !restoreLeafenEntry(entry.Name(), entry.IsDir()) {
			return entry.Name(), nil
		}
	}
	return "", nil
}

func validateBackupArchive(path string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	dataDBCount := 0
	manifestCount := 0
	var manifestFile *zip.File
	for _, file := range reader.File {
		if err := validateBackupArchiveEntry(file); err != nil {
			return err
		}
		name := filepath.ToSlash(file.Name)
		if isRetiredProductArchiveEntry(name) {
			return errRetiredProductData
		}
		if name == "data.db" {
			dataDBCount++
		}
		if name == backupManifestPath {
			manifestCount++
			manifestFile = file
		}
	}
	if dataDBCount != 1 {
		return errors.New("backup must contain one root data.db")
	}
	if manifestCount != 1 {
		return errors.New("backup must contain one Leafen format manifest")
	}
	if err := validateBackupManifest(manifestFile); err != nil {
		return err
	}
	return nil
}

func validateBackupArchiveEntry(file *zip.File) error {
	name := filepath.ToSlash(file.Name)
	if strings.Contains(file.Name, `\`) || filepath.IsAbs(file.Name) || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
		return errors.New("backup contains an invalid path")
	}
	if !file.FileInfo().IsDir() && !file.FileInfo().Mode().IsRegular() {
		return errors.New("backup contains an unsupported entry")
	}
	return nil
}

func validateBackupManifest(file *zip.File) error {
	if file == nil || file.FileInfo().IsDir() || file.UncompressedSize64 > maxBackupManifestBytes {
		return errors.New("backup manifest is invalid")
	}
	reader, err := file.Open()
	if err != nil {
		return errors.New("backup manifest is invalid")
	}
	defer reader.Close()
	decoder := json.NewDecoder(io.LimitReader(reader, maxBackupManifestBytes+1))
	decoder.DisallowUnknownFields()
	var manifest backupManifest
	if err := decoder.Decode(&manifest); err != nil {
		return errors.New("backup manifest is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("backup manifest is invalid")
	}
	if isRetiredProductFormat(manifest.Format) {
		return errRetiredProductData
	}
	if manifest.Format != backupFormat || manifest.FormatVersion != backupFormatVersion {
		return errors.New("backup format is unsupported")
	}
	return nil
}

func fileSHA256(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hasher := sha256.New()
	bytesRead, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), bytesRead, nil
}

func pathWithin(root string, candidate string) bool {
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(rootPath, candidate)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))
}
