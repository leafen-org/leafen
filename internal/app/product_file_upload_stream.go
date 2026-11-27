package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"sync"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/filesystem/blob"
)

const productFileUploadPendingDigest = "0000000000000000000000000000000000000000000000000000000000000000"

type productFileUploadPartsFileReader struct {
	app       core.App
	parts     []storedProductFileUploadPart
	totalSize int64

	mutex    sync.Mutex
	digest   string
	verified bool
	observer func(string)
}

func newProductFileUploadPartsFileReader(
	app core.App,
	parts []storedProductFileUploadPart,
	totalSize int64,
) *productFileUploadPartsFileReader {
	return &productFileUploadPartsFileReader{
		app: app, parts: append([]storedProductFileUploadPart(nil), parts...), totalSize: totalSize,
	}
}

func (r *productFileUploadPartsFileReader) Open() (io.ReadSeekCloser, error) {
	fsys, err := r.app.NewFilesystem()
	if err != nil {
		return nil, err
	}
	r.clearVerified()
	return &productFileUploadPartStream{
		owner: r, filesystem: fsys, wholeHasher: sha256.New(),
	}, nil
}

func (r *productFileUploadPartsFileReader) VerifiedDigest() (string, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if !r.verified || !sha256Pattern.MatchString(r.digest) {
		return "", errProductFileIntegrity
	}
	return r.digest, nil
}

func (r *productFileUploadPartsFileReader) clearVerified() {
	r.mutex.Lock()
	r.digest = ""
	r.verified = false
	r.mutex.Unlock()
}

func (r *productFileUploadPartsFileReader) setVerifiedObserver(observer func(string)) {
	r.mutex.Lock()
	r.observer = observer
	r.mutex.Unlock()
}

func (r *productFileUploadPartsFileReader) markVerified(digest string) {
	r.mutex.Lock()
	r.digest = digest
	r.verified = true
	observer := r.observer
	r.mutex.Unlock()
	if observer != nil {
		observer(digest)
	}
}

type productFileUploadPartStream struct {
	owner       *productFileUploadPartsFileReader
	filesystem  *filesystem.System
	partReader  *blob.Reader
	partHasher  hash.Hash
	wholeHasher hash.Hash
	partIndex   int
	partRead    int64
	totalRead   int64
	closed      bool
	terminal    bool
}

func (s *productFileUploadPartStream) Read(buffer []byte) (int, error) {
	if s.closed {
		return 0, errors.New("file upload part stream is closed")
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	for {
		if s.partIndex == len(s.owner.parts) {
			return s.finish()
		}
		part := s.owner.parts[s.partIndex]
		if s.partReader == nil {
			reader, err := s.filesystem.GetReader(part.StorageKey)
			if err != nil {
				return 0, err
			}
			s.partReader = reader
			s.partHasher = sha256.New()
		}
		remaining := part.Size - s.partRead
		if remaining == 0 {
			if err := s.finishPart(part); err != nil {
				return 0, err
			}
			continue
		}
		limit := len(buffer)
		if int64(limit) > remaining {
			limit = int(remaining)
		}
		read, err := s.partReader.Read(buffer[:limit])
		if read > 0 {
			_, _ = s.partHasher.Write(buffer[:read])
			_, _ = s.wholeHasher.Write(buffer[:read])
			s.partRead += int64(read)
			s.totalRead += int64(read)
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return read, errors.Join(errProductFileIntegrity, err)
		}
		if errors.Is(err, io.EOF) && s.partRead != part.Size {
			return read, errProductFileIntegrity
		}
		if read == 0 && err == nil {
			return 0, io.ErrNoProgress
		}
		if read > 0 {
			return read, nil
		}
	}
}

func (s *productFileUploadPartStream) finishPart(part storedProductFileUploadPart) error {
	var extra [1]byte
	read, readErr := s.partReader.Read(extra[:])
	closeErr := s.partReader.Close()
	s.partReader = nil
	if read != 0 || (readErr != nil && !errors.Is(readErr, io.EOF)) || closeErr != nil ||
		hex.EncodeToString(s.partHasher.Sum(nil)) != part.SHA256 {
		return errors.Join(errProductFileIntegrity, readErr, closeErr)
	}
	s.partIndex++
	s.partRead = 0
	s.partHasher = nil
	return nil
}

func (s *productFileUploadPartStream) finish() (int, error) {
	if s.terminal {
		return 0, io.EOF
	}
	s.terminal = true
	digest := hex.EncodeToString(s.wholeHasher.Sum(nil))
	if s.totalRead != s.owner.totalSize || !sha256Pattern.MatchString(digest) {
		return 0, errProductFileIntegrity
	}
	s.owner.markVerified(digest)
	return 0, io.EOF
}

func (s *productFileUploadPartStream) Seek(offset int64, whence int) (int64, error) {
	if s.closed {
		return 0, errors.New("file upload part stream is closed")
	}
	if whence == io.SeekCurrent && offset == 0 {
		return s.totalRead, nil
	}
	if whence != io.SeekStart || offset != 0 {
		return s.totalRead, errors.New("file upload part stream supports rewind only")
	}
	if s.partReader != nil {
		if err := s.partReader.Close(); err != nil {
			return s.totalRead, err
		}
	}
	s.partReader = nil
	s.partHasher = nil
	s.wholeHasher = sha256.New()
	s.partIndex = 0
	s.partRead = 0
	s.totalRead = 0
	s.terminal = false
	s.owner.clearVerified()
	return 0, nil
}

func (s *productFileUploadPartStream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	var readerErr error
	if s.partReader != nil {
		readerErr = s.partReader.Close()
	}
	return errors.Join(readerErr, s.filesystem.Close())
}
