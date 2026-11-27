package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/benbjohnson/litestream"
	"github.com/superfly/ltx"
)

// The continuous replica keeps the database as Litestream's LTX files in the
// backups' own place (decided 2026-10-17): under "Leafen backups/replica/db/
// <level>/", each named by the transactions it holds and sealed with the
// backups' key where they have a password. The place is any the scheduled
// backups can use — a folder, a device's folder, WebDAV or S3 — so the
// replica needs no storage client of its own.

const (
	replicaFolderName   = "replica"
	replicaDatabaseName = "db"
	replicaClientType   = "leafen-backups"
)

// replicaStoreClient is Litestream's replica client over a backup store.
type replicaStoreClient struct {
	store scheduledBackupStore
	// root is the replica's database folder in the store.
	root string
	key  *scheduledBackupKey
	// spool is a local folder where a file Litestream streams waits whole
	// before it goes out, so a stream cut off leaves nothing there.
	spool string
	// guard says, before each write, that the replica is still this
	// server's to write.
	guard  func(context.Context) error
	logger *slog.Logger

	mu      sync.Mutex
	lastErr error
}

// lastError is the last write's failure, or nil once one went.
func (client *replicaStoreClient) lastError() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.lastErr
}

func (client *replicaStoreClient) noteWrite(err error) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.lastErr = err
	return err
}

var _ litestream.ReplicaClient = (*replicaStoreClient)(nil)

func (client *replicaStoreClient) Type() string                  { return replicaClientType }
func (client *replicaStoreClient) Init(context.Context) error    { return nil }
func (client *replicaStoreClient) SetLogger(logger *slog.Logger) { client.logger = logger }
func (client *replicaStoreClient) levelFolder(level int) string {
	return scheduledBackupStoreJoin(client.root, strconv.Itoa(level))
}
func (client *replicaStoreClient) fileName(min, max ltx.TXID) string {
	return client.key.fileName(ltx.FormatFilename(min, max))
}

func (client *replicaStoreClient) filePath(level int, minTXID, maxTXID ltx.TXID) string {
	return scheduledBackupStoreJoin(client.levelFolder(level), client.fileName(minTXID, maxTXID))
}

// LTXFiles lists a level's files from seek on. A store that keeps no time
// for its files gives them the time now, so none looks old enough to go.
func (client *replicaStoreClient) LTXFiles(_ context.Context, level int, seek ltx.TXID, _ bool) (ltx.FileIterator, error) {
	entries, err := client.store.List(client.levelFolder(level))
	if errors.Is(err, fs.ErrNotExist) {
		return ltx.NewFileInfoSliceIterator(nil), nil
	}
	if err != nil {
		return nil, err
	}
	infos := make([]*ltx.FileInfo, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name
		if client.key != nil {
			if !strings.HasSuffix(name, ".sealed") {
				continue
			}
			name = strings.TrimSuffix(name, ".sealed")
		}
		minTXID, maxTXID, err := ltx.ParseFilename(name)
		if entry.Dir || err != nil || minTXID < seek {
			continue
		}
		created := entry.Modified.UTC()
		if created.IsZero() {
			created = time.Now().UTC()
		}
		size := entry.Size
		if client.key != nil {
			size = sealedPlainSize(size)
		}
		infos = append(infos, &ltx.FileInfo{Level: level, MinTXID: minTXID, MaxTXID: maxTXID, Size: size, CreatedAt: created})
	}
	return ltx.NewFileInfoSliceIterator(infos), nil
}

// sealedPlainSize is a sealed file's size before it was sealed: Litestream
// reads files by their listed size. A sealed file is its header, then each
// piece of up to 64 KiB with its tag; an empty file is one empty piece.
func sealedPlainSize(sealed int64) int64 {
	pieces := sealed - int64(len(scheduledBackupSealedMagic)+8+7)
	if pieces <= 0 {
		return 0
	}
	const overhead = 16
	count := (pieces + scheduledBackupSealedPiece + overhead - 1) / (scheduledBackupSealedPiece + overhead)
	return max(pieces-count*overhead, 0)
}

// OpenLTXFile opens a file as it was written; a part of a sealed one is
// read from its start.
func (client *replicaStoreClient) OpenLTXFile(_ context.Context, level int, minTXID, maxTXID ltx.TXID, offset, size int64) (io.ReadCloser, error) {
	path := client.filePath(level, minTXID, maxTXID)
	reader, err := client.key.reader(client.store, path)
	if err != nil {
		return nil, litestream.NewLTXError("open", path, level, uint64(minTXID), uint64(maxTXID), err)
	}
	if offset > 0 {
		if _, err := io.CopyN(io.Discard, reader, offset); err != nil {
			return nil, errors.Join(err, reader.Close())
		}
	}
	if size > 0 {
		return struct {
			io.Reader
			io.Closer
		}{io.LimitReader(reader, size), reader}, nil
	}
	return reader, nil
}

// WriteLTXFile writes a file whole under its name: one already there, as
// one a cut-off attempt left, gives way.
func (client *replicaStoreClient) WriteLTXFile(ctx context.Context, level int, minTXID, maxTXID ltx.TXID, source io.Reader) (*ltx.FileInfo, error) {
	info, err := client.writeLTXFile(ctx, level, minTXID, maxTXID, source)
	return info, client.noteWrite(err)
}

func (client *replicaStoreClient) writeLTXFile(ctx context.Context, level int, minTXID, maxTXID ltx.TXID, source io.Reader) (*ltx.FileInfo, error) {
	if client.guard != nil {
		if err := client.guard(ctx); err != nil {
			return nil, err
		}
	}
	local, size, cleanup, err := client.wholeLocally(source)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	header, _, err := ltx.PeekHeader(local)
	if err != nil {
		return nil, fmt.Errorf("read the LTX header: %w", err)
	}
	if _, err := local.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if err := client.store.MakeDir(client.levelFolder(level)); err != nil {
		return nil, err
	}
	path := client.filePath(level, minTXID, maxTXID)
	output, err := client.store.Create(path)
	if errors.Is(err, fs.ErrExist) {
		if err = client.store.RemoveAll(path); err == nil {
			output, err = client.store.Create(path)
		}
	}
	if err != nil {
		return nil, err
	}
	sealed, err := client.key.writer(output)
	if err != nil {
		return nil, errors.Join(err, output.Close(), client.store.RemoveAll(path))
	}
	_, copyErr := io.Copy(sealed, local)
	if err := errors.Join(copyErr, sealed.Close(), output.Close()); err != nil {
		return nil, errors.Join(err, client.store.RemoveAll(path))
	}
	return &ltx.FileInfo{Level: level, MinTXID: minTXID, MaxTXID: maxTXID, Size: size,
		CreatedAt: time.UnixMilli(header.Timestamp).UTC()}, nil
}

// wholeLocally gives a file Litestream hands over as a local file read from
// its start: its own file as it is, a stream copied to the spool first.
func (client *replicaStoreClient) wholeLocally(source io.Reader) (io.ReadSeeker, int64, func(), error) {
	if file, ok := source.(*os.File); ok {
		info, err := file.Stat()
		if err != nil {
			return nil, 0, nil, err
		}
		return file, info.Size(), func() {}, nil
	}
	if err := os.MkdirAll(client.spool, 0o700); err != nil {
		return nil, 0, nil, err
	}
	spooled, err := os.CreateTemp(client.spool, "replica-ltx-*")
	if err != nil {
		return nil, 0, nil, err
	}
	cleanup := func() { _ = spooled.Close(); _ = os.Remove(spooled.Name()) }
	size, err := io.Copy(spooled, source)
	if err == nil {
		_, err = spooled.Seek(0, io.SeekStart)
	}
	if err != nil {
		cleanup()
		return nil, 0, nil, err
	}
	return spooled, size, cleanup, nil
}

// DeleteLTXFiles removes files compaction and retention let go.
func (client *replicaStoreClient) DeleteLTXFiles(_ context.Context, infos []*ltx.FileInfo) error {
	for _, info := range infos {
		if err := client.store.RemoveAll(client.filePath(info.Level, info.MinTXID, info.MaxTXID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// DeleteAll removes the replica's database files.
func (client *replicaStoreClient) DeleteAll(context.Context) error {
	if err := client.store.RemoveAll(client.root); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// replicaLatestChange is the replica's newest transaction and when it was
// written, over every level (compaction moves level-0 files up), or zero
// with none.
func replicaLatestChange(ctx context.Context, client *replicaStoreClient) (time.Time, ltx.TXID, error) {
	var latest time.Time
	var txid ltx.TXID
	for level := 0; level <= litestream.SnapshotLevel; level++ {
		iterator, err := client.LTXFiles(ctx, level, 0, false)
		if err != nil {
			return time.Time{}, 0, err
		}
		for iterator.Next() {
			if item := iterator.Item(); item.MaxTXID > txid || (item.MaxTXID == txid && item.CreatedAt.After(latest)) {
				txid, latest = item.MaxTXID, item.CreatedAt
			}
		}
		if err := iterator.Close(); err != nil {
			return time.Time{}, 0, err
		}
	}
	return latest, txid, nil
}
