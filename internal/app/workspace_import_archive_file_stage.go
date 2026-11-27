package app

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const workspaceImportArchiveFileKind = "file"

func inventoryWorkspaceImportArchiveFiles(
	descriptor workspaceExportArchiveDescriptor,
	indexEntry, dataEntry *zip.File,
	jobID, fileSalt, directory string,
	mapping *sql.DB,
) (storedWorkspaceImportSource, error) {
	included, _ := descriptor.Files["included"].(bool)
	count, countOK := jsonNumberInt(descriptor.Files["count"])
	bytesCount, bytesOK := jsonNumberInt(descriptor.Files["bytes"])
	stripped, strippedOK := jsonNumberInt(descriptor.Files["strippedReferences"])
	indexStream := descriptor.Streams["fileIndex"]
	dataStream := descriptor.Streams["fileData"]
	filesMismatch := workspaceImportInvalid(workspaceExportDescriptorPath + "'s files (included, count, bytes, strippedReferences 0) don't match " +
		workspaceExportFilesIndexPath + " and " + workspaceExportFilesDataPath)
	if !included || !countOK || !bytesOK || !strippedOK || stripped != 0 ||
		len(descriptor.Files) != 4 || indexStream.Records != count ||
		dataStream.Records != 0 || int64(dataEntry.UncompressedSize64) != int64(bytesCount) {
		return storedWorkspaceImportSource{}, filesMismatch
	}
	if count == 0 {
		if bytesCount != 0 || indexStream.Bytes != 0 || dataStream.Bytes != 0 {
			return storedWorkspaceImportSource{}, workspaceImportInputError()
		}
		if err := verifyEmptyWorkspaceImportArchiveFileStreams(indexEntry, dataEntry, indexStream, dataStream); err != nil {
			return storedWorkspaceImportSource{}, err
		}
		return storedWorkspaceImportSource{}, nil
	}
	if bytesCount < 1 || indexStream.Bytes < 1 || dataStream.Bytes != int64(bytesCount) {
		return storedWorkspaceImportSource{}, filesMismatch
	}

	indexReader, err := indexEntry.Open()
	if err != nil {
		return storedWorkspaceImportSource{}, workspaceImportInputError()
	}
	defer indexReader.Close()
	dataReader, err := dataEntry.Open()
	if err != nil {
		return storedWorkspaceImportSource{}, workspaceImportInputError()
	}
	defer dataReader.Close()

	storageKey := jobID + ".files.ndjson"
	temporaryPath := filepath.Join(directory, "."+storageKey+".tmp")
	finalPath := filepath.Join(directory, storageKey)
	_ = os.Remove(temporaryPath)
	file, err := os.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return storedWorkspaceImportSource{}, workspaceImportStorageErrorAt(err, "archive-file-index-create")
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = file.Close()
			_ = os.Remove(temporaryPath)
		}
	}()

	indexHasher := sha256.New()
	dataHasher := sha256.New()
	normalizedHasher := sha256.New()
	buffered := bufio.NewReaderSize(io.TeeReader(indexReader, indexHasher), workspaceExportReaderWindowBytes)
	data := io.TeeReader(dataReader, dataHasher)
	buffer := make([]byte, workspaceExportReaderWindowBytes)
	var offset int64
	seen := 0
	chain := ""
	for {
		line, readErr := buffered.ReadBytes('\n')
		if len(line) > 0 {
			place := fmt.Sprintf("%s line %d", workspaceExportFilesIndexPath, seen+1)
			if line[len(line)-1] != '\n' || len(line) == 1 || !utf8.Valid(line[:len(line)-1]) {
				return storedWorkspaceImportSource{}, workspaceImportAt(workspaceImportInvalid(workspaceImportArchiveLineShape), place)
			}
			entry, err := decodeWorkspaceImportArchiveFile(line[:len(line)-1], fileSalt, offset)
			if err != nil {
				return storedWorkspaceImportSource{}, workspaceImportAt(err, place)
			}
			fileHasher := sha256.New()
			written, copyErr := io.CopyBuffer(
				fileHasher, io.LimitReader(data, entry.Bytes), buffer,
			)
			if copyErr != nil || written != entry.Bytes || hex.EncodeToString(fileHasher.Sum(nil)) != entry.SHA256 {
				return storedWorkspaceImportSource{}, workspaceImportAt(workspaceImportInvalid(
					"the file's bytes in "+workspaceExportFilesDataPath+" aren't its bytes count long with its sha256"), place)
			}
			if err := storeWorkspaceImportArchiveFile(mapping, entry); err != nil {
				return storedWorkspaceImportSource{}, err
			}
			canonical, err := json.Marshal(entry)
			if err != nil {
				return storedWorkspaceImportSource{}, workspaceImportInputError()
			}
			chainDigest := sha256.Sum256(append([]byte(chain), canonical...))
			chain = hex.EncodeToString(chainDigest[:])
			entry.ChainSHA256 = chain
			encoded, err := json.Marshal(entry)
			if err != nil {
				return storedWorkspaceImportSource{}, workspaceImportInputError()
			}
			encoded = append(encoded, '\n')
			if written, err := io.MultiWriter(file, normalizedHasher).Write(encoded); err != nil || written != len(encoded) {
				return storedWorkspaceImportSource{}, workspaceImportStorageErrorAt(err, "archive-file-index-write")
			}
			offset += entry.Bytes
			seen++
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return storedWorkspaceImportSource{}, workspaceImportInputError()
		}
	}
	extra := make([]byte, 1)
	extraCount, extraErr := data.Read(extra)
	if extraErr != nil && !errors.Is(extraErr, io.EOF) {
		return storedWorkspaceImportSource{}, workspaceImportInputError()
	}
	if extraCount != 0 || seen != count || offset != int64(bytesCount) ||
		hex.EncodeToString(indexHasher.Sum(nil)) != indexStream.SHA256 ||
		hex.EncodeToString(dataHasher.Sum(nil)) != dataStream.SHA256 {
		return storedWorkspaceImportSource{}, workspaceImportInvalid(fmt.Sprintf(
			"%s has %d lines for %d bytes of files; %s says %d files, %d bytes and each entry's SHA-256",
			workspaceExportFilesIndexPath, seen, offset, workspaceExportDescriptorPath, count, bytesCount))
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return storedWorkspaceImportSource{}, workspaceImportStorageErrorAt(err, "archive-file-index-sync")
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return storedWorkspaceImportSource{}, workspaceImportStorageErrorAt(err, "archive-file-index-rename")
	}
	removeTemporary = false
	if err := syncDirectory(directory); err != nil {
		_ = os.Remove(finalPath)
		return storedWorkspaceImportSource{}, workspaceImportStorageErrorAt(err, "archive-file-index-directory-sync")
	}
	return storedWorkspaceImportSource{
		FileIndexStorageKey: storageKey,
		FileIndexSHA256:     hex.EncodeToString(normalizedHasher.Sum(nil)),
		FileCount:           count,
		FileBytes:           offset,
	}, nil
}

func decodeWorkspaceImportArchiveFile(raw []byte, fileSalt string, expectedOffset int64) (workspaceImportArchiveFile, error) {
	source := workspaceExportFileLine{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&source) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return workspaceImportArchiveFile{}, workspaceImportInvalid(
			"not one file's line: id, offset, name, bytes, sha256, scope and contentType, and nothing else")
	}
	var reason string
	switch {
	case !workspaceExportFileRecordIDPattern.MatchString(source.ID):
		reason = "id isn't a file ID of 15 letters, digits, '-' or '_'"
	case source.Offset != expectedOffset:
		reason = "offset isn't where the file's bytes start in " + workspaceExportFilesDataPath + " (the bytes of the lines before it)"
	case strings.TrimSpace(source.Name) != source.Name || source.Name == "" || len([]rune(source.Name)) > 2000:
		reason = "name is empty, starts or ends with a space, or is longer than 2000 characters"
	case source.Bytes < 1 || source.Bytes > fileTransferMaxFileBytes:
		reason = "bytes isn't between 1 and the largest file Leafen keeps"
	case !sha256Pattern.MatchString(source.SHA256):
		reason = "sha256 isn't 64 lowercase hexadecimal digits"
	case source.Scope != "icons" && source.Scope != "covers" && source.Scope != "uploads":
		reason = "scope isn't icons, covers or uploads"
	}
	if reason != "" {
		return workspaceImportArchiveFile{}, workspaceImportInvalid(reason)
	}
	input := fileUploadPrepareInput{
		RequestID: "archive-file", WorkspaceID: "archive-workspace",
		Name: source.Name, Size: source.Bytes, ContentType: source.ContentType,
	}
	if normalizeProductFileUploadInput(&input) != nil || input.ContentType != source.ContentType {
		return workspaceImportArchiveFile{}, workspaceImportInvalid("contentType isn't the type Leafen keeps for a file of that name")
	}
	return workspaceImportArchiveFile{
		SourceID: source.ID,
		TargetID: workspaceImportArchiveMappedID(fileSalt, workspaceImportArchiveFileKind, source.ID),
		Offset:   source.Offset, Name: source.Name, ContentType: source.ContentType,
		Scope: source.Scope, Bytes: source.Bytes, SHA256: source.SHA256,
	}, nil
}

func storeWorkspaceImportArchiveFile(mapping *sql.DB, file workspaceImportArchiveFile) error {
	tx, err := mapping.Begin()
	if err != nil {
		return workspaceImportStorageErrorAt(err, "archive-file-map-begin")
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT INTO mappings (source_id, target_id, kind) VALUES (?, ?, ?)`,
		file.SourceID, file.TargetID, workspaceImportArchiveFileKind,
	); err != nil {
		return workspaceImportInputError()
	}
	if _, err := tx.Exec(
		`INSERT INTO files (source_id, target_id, offset, name, content_type, scope, bytes, sha256)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		file.SourceID, file.TargetID, file.Offset, file.Name, file.ContentType,
		file.Scope, file.Bytes, file.SHA256,
	); err != nil {
		return workspaceImportInputError()
	}
	if err := tx.Commit(); err != nil {
		return workspaceImportStorageErrorAt(err, "archive-file-map-commit")
	}
	return nil
}

func verifyEmptyWorkspaceImportArchiveFileStreams(
	indexEntry, dataEntry *zip.File,
	indexStream, dataStream workspaceExportStreamDescriptor,
) error {
	for _, item := range []struct {
		entry  *zip.File
		stream workspaceExportStreamDescriptor
	}{{indexEntry, indexStream}, {dataEntry, dataStream}} {
		if item.stream.Records != 0 || item.stream.Bytes != 0 || item.entry.UncompressedSize64 != 0 {
			return workspaceImportInputError()
		}
		reader, err := item.entry.Open()
		if err != nil {
			return workspaceImportInputError()
		}
		hasher := sha256.New()
		written, copyErr := io.Copy(hasher, reader)
		closeErr := reader.Close()
		if copyErr != nil || closeErr != nil || written != 0 || hex.EncodeToString(hasher.Sum(nil)) != item.stream.SHA256 {
			return workspaceImportInputError()
		}
	}
	return nil
}
