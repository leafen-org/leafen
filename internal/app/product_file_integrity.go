package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

const (
	errorCodeFileIntegrityFailed = "LEAFEN_FILE_INTEGRITY_FAILED"
	productFileDigestBufferSize  = 128 * 1024
)

var errProductFileIntegrity = errors.New(errorCodeFileIntegrityFailed)

func settleProductFileStorageIdentity(app core.App, record *core.Record) error {
	fsys, err := app.NewFilesystem()
	if err != nil {
		return err
	}
	defer fsys.Close()

	attributes, err := fsys.Attributes(productFileStorageKey(record))
	if err != nil || !productFileAttributesMatchRecord(attributes.Size, attributes.ETag, record) {
		return errProductFileIntegrity
	}
	record.Set("storage_etag", attributes.ETag)
	return app.Save(record)
}

func verifyProductFileStorageIdentity(app core.App, fsys *filesystem.System, record *core.Record) error {
	fileKey := productFileStorageKey(record)
	attributes, err := fsys.Attributes(fileKey)
	if err != nil || !productFileAttributesMatchRecord(attributes.Size, attributes.ETag, record) {
		return errProductFileIntegrity
	}

	storedETag := strings.TrimSpace(record.GetString("storage_etag"))
	if storedETag == attributes.ETag {
		return nil
	}
	if err := verifyProductFileDigest(fsys, fileKey, record); err != nil {
		return errProductFileIntegrity
	}

	confirmed, err := fsys.Attributes(fileKey)
	if err != nil || confirmed.Size != attributes.Size || confirmed.ETag != attributes.ETag {
		return errProductFileIntegrity
	}
	if err := rebindProductFileStorageIdentity(app, record, storedETag, confirmed.ETag); err != nil {
		return errProductFileIntegrity
	}
	return nil
}

func productFileAttributesMatchRecord(size int64, etag string, record *core.Record) bool {
	return record != nil && strings.TrimSpace(etag) != "" && size == int64(record.GetFloat("size"))
}

func productFileStorageKey(record *core.Record) string {
	return record.BaseFilesPath() + "/" + record.GetString("blob")
}

func verifyProductFileDigest(fsys *filesystem.System, fileKey string, record *core.Record) error {
	expectedSize := int64(record.GetFloat("size"))
	expectedDigest := strings.ToLower(strings.TrimSpace(record.GetString("sha256")))
	if expectedSize <= 0 || !sha256Pattern.MatchString(expectedDigest) {
		return errProductFileIntegrity
	}

	reader, err := fsys.GetReader(fileKey)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	written, copyErr := io.CopyBuffer(
		hasher,
		io.LimitReader(reader, expectedSize+1),
		make([]byte, productFileDigestBufferSize),
	)
	closeErr := reader.Close()
	if copyErr != nil || closeErr != nil || written != expectedSize ||
		hex.EncodeToString(hasher.Sum(nil)) != expectedDigest {
		return errProductFileIntegrity
	}
	return nil
}

func rebindProductFileStorageIdentity(app core.App, record *core.Record, previousETag string, currentETag string) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_files}}
		SET [[storage_etag]] = {:currentETag}
		WHERE [[id]] = {:id} AND [[storage_etag]] = {:previousETag}
			AND [[blob]] = {:blob} AND [[size]] = {:size} AND [[sha256]] = {:sha256}
	`).Bind(dbx.Params{
		"id": record.Id, "previousETag": previousETag, "currentETag": currentETag,
		"blob": record.GetString("blob"), "size": int64(record.GetFloat("size")),
		"sha256": record.GetString("sha256"),
	}).Execute()
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return errProductFileIntegrity
	}
	record.Set("storage_etag", currentETag)
	return nil
}

func isProductFileIntegrityError(err error) bool {
	return errors.Is(err, errProductFileIntegrity)
}
