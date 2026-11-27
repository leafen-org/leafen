package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/filesystem/blob"
)

// s3BackupStore keeps scheduled backups in a bucket of S3-compatible storage
// (user decision 2026-10-03: B2, R2, MinIO and the like), under a prefix,
// in the same layout as a folder. A folder is only a prefix there, made by
// what is put under it; an object has no rename, so a move copies and then
// deletes, a snapshot's list last, and a snapshot is whole once its list is
// there.
type s3BackupStore struct {
	files  *filesystem.System
	prefix string
}

var (
	errS3BackupUnauthorized = errors.New(errorCodeScheduledBackupS3Unauthorized)
	errS3BackupInvalid      = errors.New(errorCodeScheduledBackupS3Invalid)
)

// newS3BackupStore opens a bucket at an http or https endpoint; a prefix
// keeps the backups to a part of it.
func newS3BackupStore(ctx context.Context, endpoint, region, bucket, prefix, accessKey, secretKey string, pathStyle bool) (*s3BackupStore, error) {
	address, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || (address.Scheme != "https" && address.Scheme != "http") || address.Host == "" || address.User != nil ||
		address.RawQuery != "" || address.Fragment != "" || !s3BackupBucketValid(bucket) || strings.TrimSpace(region) == "" {
		return nil, errS3BackupInvalid
	}
	files, err := filesystem.NewS3(bucket, strings.TrimSpace(region), strings.TrimSuffix(address.String(), "/"),
		accessKey, secretKey, pathStyle)
	if err != nil {
		return nil, errS3BackupInvalid
	}
	if ctx != nil {
		files.SetContext(ctx)
	}
	return &s3BackupStore{files: files, prefix: strings.Trim(strings.TrimSpace(prefix), "/")}, nil
}

// s3BackupBucketValid is a bucket name as S3 allows it: 3 to 63 lowercase
// letters, digits, dots and hyphens, starting and ending with a letter or
// digit.
func s3BackupBucketValid(bucket string) bool {
	if len(bucket) < 3 || len(bucket) > 63 {
		return false
	}
	for index, char := range bucket {
		edge := index == 0 || index == len(bucket)-1
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
		case (char == '.' || char == '-') && !edge:
		default:
			return false
		}
	}
	return true
}

func (store *s3BackupStore) key(relative string) string {
	return strings.Trim(path.Join(store.prefix, relative), "/")
}

// s3BackupError names what an S3 answer means here: a missing object as
// fs.ErrNotExist, refused keys apart from the rest.
func s3BackupError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, blob.ErrNotFound) {
		return errors.Join(err, fs.ErrNotExist)
	}
	switch s3BackupStatus(err) {
	case 401, 403:
		return errors.Join(err, errS3BackupUnauthorized)
	}
	return err
}

// s3BackupStatus is the HTTP status of the S3 answer an error carries
// (PocketBase's client keeps it in a ResponseError of its own package), or 0.
func s3BackupStatus(err error) int {
	queue := []error{err}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == nil {
			continue
		}
		value := reflect.ValueOf(current)
		if value.Kind() == reflect.Pointer && !value.IsNil() && value.Elem().Kind() == reflect.Struct &&
			value.Elem().Type().Name() == "ResponseError" {
			if status := value.Elem().FieldByName("Status"); status.IsValid() && status.CanInt() {
				return int(status.Int())
			}
		}
		switch wrapped := current.(type) {
		case interface{ Unwrap() []error }:
			queue = append(queue, wrapped.Unwrap()...)
		case interface{ Unwrap() error }:
			queue = append(queue, wrapped.Unwrap())
		}
	}
	return 0
}

// MakeDir has nothing to make: a prefix is there once something is under it.
func (store *s3BackupStore) MakeDir(string) error { return nil }

func (store *s3BackupStore) Create(relative string) (io.WriteCloser, error) {
	exists, err := store.files.Exists(store.key(relative))
	if err != nil {
		return nil, s3BackupError(err)
	}
	if exists {
		return nil, fs.ErrExist
	}
	writer, err := store.files.NewWriter(store.key(relative), &blob.WriterOptions{ContentType: "application/octet-stream"})
	if err != nil {
		return nil, s3BackupError(err)
	}
	return s3BackupWriter{writer}, nil
}

type s3BackupWriter struct{ *blob.Writer }

func (writer s3BackupWriter) Close() error { return s3BackupError(writer.Writer.Close()) }

func (store *s3BackupStore) Open(relative string) (io.ReadCloser, error) {
	reader, err := store.files.GetReader(store.key(relative))
	if err != nil {
		return nil, s3BackupError(err)
	}
	return reader, nil
}

// Stat finds an object, or else a prefix with objects under it.
func (store *s3BackupStore) Stat(relative string) (scheduledBackupStoreEntry, error) {
	name := path.Base("/" + relative)
	attributes, err := store.files.Attributes(store.key(relative))
	if err == nil {
		return scheduledBackupStoreEntry{Name: name, Size: attributes.Size, Modified: attributes.ModTime}, nil
	}
	if err = s3BackupError(err); !errors.Is(err, fs.ErrNotExist) {
		return scheduledBackupStoreEntry{}, err
	}
	objects, err := store.objects(relative)
	if err != nil {
		return scheduledBackupStoreEntry{}, err
	}
	if len(objects) == 0 {
		return scheduledBackupStoreEntry{}, fs.ErrNotExist
	}
	entry := scheduledBackupStoreEntry{Name: name, Dir: true}
	for _, object := range objects {
		if object.ModTime.After(entry.Modified) {
			entry.Modified = object.ModTime
		}
	}
	return entry, nil
}

// objects lists every object under a prefix, with keys relative to it.
func (store *s3BackupStore) objects(relative string) ([]*blob.ListObject, error) {
	prefix := store.key(relative)
	if prefix != "" {
		prefix += "/"
	}
	listed, err := store.files.List(prefix)
	if err != nil {
		return nil, s3BackupError(err)
	}
	for _, object := range listed {
		object.Key = strings.TrimPrefix(object.Key, prefix)
	}
	return listed, nil
}

// Rename copies what is at from to to and then deletes it; a snapshot's
// list goes last, so the snapshot is whole only once everything is there.
func (store *s3BackupStore) Rename(from, to string) error {
	if scheduledBackupStoreExists(store, to) {
		return fs.ErrExist
	}
	if exists, err := store.files.Exists(store.key(from)); err != nil {
		return s3BackupError(err)
	} else if exists {
		if err := store.files.Copy(store.key(from), store.key(to)); err != nil {
			return s3BackupError(err)
		}
		return s3BackupError(store.files.Delete(store.key(from)))
	}
	objects, err := store.objects(from)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		return fs.ErrNotExist
	}
	sort.SliceStable(objects, func(left, right int) bool {
		return !s3BackupListLast(objects[left].Key) && s3BackupListLast(objects[right].Key)
	})
	for _, object := range objects {
		if err := store.files.Copy(store.key(path.Join(from, object.Key)), store.key(path.Join(to, object.Key))); err != nil {
			return s3BackupError(err)
		}
	}
	return store.RemoveAll(from)
}

func s3BackupListLast(key string) bool {
	return strings.HasPrefix(path.Base(key), scheduledBackupManifestName)
}

// List lists a prefix's own entries: its objects, and the prefixes one step
// below it as folders.
func (store *s3BackupStore) List(relative string) ([]scheduledBackupStoreEntry, error) {
	objects, err := store.objects(relative)
	if err != nil {
		return nil, err
	}
	if len(objects) == 0 {
		return nil, fs.ErrNotExist
	}
	folders := map[string]int{}
	listed := []scheduledBackupStoreEntry{}
	for _, object := range objects {
		first, rest, nested := strings.Cut(object.Key, "/")
		if first == "" {
			continue
		}
		if !nested || rest == "" {
			listed = append(listed, scheduledBackupStoreEntry{Name: first, Size: object.Size, Modified: object.ModTime})
			continue
		}
		index, seen := folders[first]
		if !seen {
			index = len(listed)
			folders[first] = index
			listed = append(listed, scheduledBackupStoreEntry{Name: first, Dir: true})
		}
		if object.ModTime.After(listed[index].Modified) {
			listed[index].Modified = object.ModTime
		}
	}
	return listed, nil
}

func (store *s3BackupStore) RemoveAll(relative string) error {
	prefix := store.key(relative)
	// The whole bucket is never what a backup removes.
	if prefix == "" || prefix == store.prefix {
		return errors.New("refusing to remove the backups' own root")
	}
	if err := store.files.Delete(prefix); err != nil && !errors.Is(err, blob.ErrNotFound) {
		return s3BackupError(err)
	}
	for _, err := range store.files.DeletePrefix(prefix + "/") {
		if err != nil && !errors.Is(err, blob.ErrNotFound) {
			return s3BackupError(err)
		}
	}
	return nil
}

func (store *s3BackupStore) Replace(relative string, content []byte) error {
	writer, err := store.files.NewWriter(store.key(relative), &blob.WriterOptions{ContentType: "application/octet-stream"})
	if err != nil {
		return s3BackupError(err)
	}
	_, writeErr := writer.Write(content)
	return s3BackupError(errors.Join(writeErr, writer.Close()))
}

// Sync has nothing to do: an object is stored once its upload answers.
func (store *s3BackupStore) Sync(string) error { return nil }

// Close lets the client go.
func (store *s3BackupStore) Close() error { return store.files.Close() }

// s3BackupCheckTimeout is how long saving S3 storage waits for its check.
const s3BackupCheckTimeout = 30 * time.Second
