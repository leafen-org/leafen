package app

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
)

// A backup folder with a password keeps everything encrypted: the database,
// each snapshot's list and every content. The password makes a key through
// PBKDF2 with a salt kept, with a check value, in "Leafen backups/keys.json";
// a new password adds a key, and a snapshot made under an old one opens only
// with that one. From the key come a content key and a name key: contents
// are named by the name key's HMAC of their SHA-256, so a folder kept once
// per content shows no content's hash. A file is sealed in 64 KiB pieces with
// AES-256-GCM, each piece's number and whether it is the last in its nonce,
// so pieces can't be reordered or cut short.
const (
	scheduledBackupKeysName       = "keys.json"
	scheduledBackupKeyIterations  = 600_000
	scheduledBackupSealedMagic    = "HJBK1"
	scheduledBackupSealedPiece    = 64 << 10
	scheduledBackupKeyCheckDomain = "leafen backup key check"
)

var errScheduledBackupPassword = errors.New("the backup password doesn't open this backup")

// errScheduledBackupPlaceForeign is a backup place holding another server's
// sealed backups, which this server hasn't opened.
var errScheduledBackupPlaceForeign = errors.New("the backup place holds backups this server can't open")

// errScheduledBackupPlaceOpened is a place whose backups this server opened
// with another server's code or password, not yet chosen to write there.
var errScheduledBackupPlaceOpened = errors.New("the backup place holds another server's backups, opened here")

// scheduledBackupPlaceForeign is whether a place holds sealed backups none of
// whose keys the secret opens: another server's, a lost one being restored
// here. Nothing of this server's is written or pruned there until they are
// opened with their code or password (2026-11-07: a fresh server's first run
// put its own empty, unsealed snapshot among them, counted against the ones
// kept). A place without keys, or one a key of this server's opens, is not.
func scheduledBackupPlaceForeign(store scheduledBackupStore, root string, secret scheduledBackupSecret) (bool, error) {
	records, err := readScheduledBackupKeyRecords(store, scheduledBackupStoreJoin(root, scheduledBackupKeysName))
	if err != nil || len(records) == 0 {
		return false, err
	}
	keys, err := scheduledBackupKeysIn(store, root, secret)
	if err != nil {
		return false, err
	}
	return len(keys) == 0, nil
}

type scheduledBackupKeyRecord struct {
	ID         string `json:"id"`
	Salt       string `json:"salt,omitempty"`
	Iterations int    `json:"iterations,omitempty"`
	Check      string `json:"check"`
	// Wraps hold a random backup key wrapped by each way to open it
	// (scheduled_backup_protection.go); a record without them is an older
	// one, its key made from a password.
	Wraps []scheduledBackupKeyWrap `json:"wraps,omitempty"`
}

type scheduledBackupKey struct {
	id      string
	content []byte
	names   []byte
}

func deriveScheduledBackupKey(password string, record scheduledBackupKeyRecord) (*scheduledBackupKey, error) {
	salt, err := hex.DecodeString(record.Salt)
	if err != nil || len(salt) < 16 || record.Iterations < 100_000 {
		return nil, errors.New("the backup's key record is not valid")
	}
	master, err := pbkdf2.Key(sha256.New, password, salt, record.Iterations, 32)
	if err != nil {
		return nil, err
	}
	content, err := hkdf.Key(sha256.New, master, nil, "leafen backup content", 32)
	if err != nil {
		return nil, err
	}
	names, err := hkdf.Key(sha256.New, master, nil, "leafen backup names", 32)
	if err != nil {
		return nil, err
	}
	return &scheduledBackupKey{id: record.ID, content: content, names: names}, nil
}

func (key *scheduledBackupKey) check() string {
	mac := hmac.New(sha256.New, key.names)
	mac.Write([]byte(scheduledBackupKeyCheckDomain))
	return hex.EncodeToString(mac.Sum(nil))
}

// blobName is a content's name in the folder: its SHA-256 itself without a
// key, its name key's HMAC with one.
func (key *scheduledBackupKey) blobName(digest string) string {
	if key == nil {
		return digest
	}
	mac := hmac.New(sha256.New, key.names)
	mac.Write([]byte(digest))
	return hex.EncodeToString(mac.Sum(nil))
}

// fileName is a snapshot file's name: sealed ones end in ".sealed".
func (key *scheduledBackupKey) fileName(name string) string {
	if key == nil {
		return name
	}
	return name + ".sealed"
}

// writer writes to output as it is, or sealed with a key.
func (key *scheduledBackupKey) writer(output io.Writer) (io.WriteCloser, error) {
	if key == nil {
		return nopWriteCloser{output}, nil
	}
	return key.sealing(output)
}

// reader opens a file of the store as it was written: sealed ones opened as
// they are read, piece by piece.
func (key *scheduledBackupKey) reader(store scheduledBackupStore, path string) (io.ReadCloser, error) {
	file, err := store.Open(path)
	if err != nil || key == nil {
		return file, err
	}
	pipeReader, pipeWriter := io.Pipe()
	go func() {
		err := key.openSealed(pipeWriter, file)
		pipeWriter.CloseWithError(errors.Join(err, file.Close()))
	}()
	return pipeReader, nil
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// scheduledBackupKeyFor finds the key a password opens in a backup root on
// the server's own file system.
func scheduledBackupKeyFor(root, password string, create bool) (*scheduledBackupKey, error) {
	return scheduledBackupKeyIn(newFolderBackupStore(root), "", passwordBackupSecret(password), create)
}

func readScheduledBackupKeyRecords(store scheduledBackupStore, path string) ([]scheduledBackupKeyRecord, error) {
	records := []scheduledBackupKeyRecord{}
	if raw, err := scheduledBackupStoreReadAll(store, path); err == nil {
		if err := json.Unmarshal(raw, &records); err != nil {
			return nil, errors.New("the backup folder's keys.json is not valid")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return records, nil
}

// scheduledBackupKeysIn are the keys of a backup root the secret opens, the
// newest first: the server's own, then wrapped ones, then older ones made
// from a password. A snapshot opens with one of them.
func scheduledBackupKeysIn(store scheduledBackupStore, root string, secret scheduledBackupSecret) ([]*scheduledBackupKey, error) {
	records, err := readScheduledBackupKeyRecords(store, scheduledBackupStoreJoin(root, scheduledBackupKeysName))
	if err != nil {
		return nil, err
	}
	keys := []*scheduledBackupKey{}
	for _, pass := range []func(scheduledBackupKeyRecord) bool{
		func(record scheduledBackupKeyRecord) bool {
			return secret.Record != nil && record.ID == secret.Record.ID
		},
		func(record scheduledBackupKeyRecord) bool {
			return record.Wraps != nil && (secret.Record == nil || record.ID != secret.Record.ID)
		},
		func(record scheduledBackupKeyRecord) bool { return record.Wraps == nil },
	} {
		for _, record := range records {
			if !pass(record) {
				continue
			}
			if key, _ := openScheduledBackupRecord(record, secret); key != nil {
				keys = append(keys, key)
			}
		}
	}
	return keys, nil
}

// scheduledBackupKeyIn finds the backup root's key the secret opens, the
// newest where several do. With create set, the server's own record is kept
// there as the server has it — so every way that opens the key here opens
// it there — and a password alone (backups the environment sets) adds a key
// made from it where none opens.
func scheduledBackupKeyIn(store scheduledBackupStore, root string, secret scheduledBackupSecret, create bool) (*scheduledBackupKey, error) {
	path := scheduledBackupStoreJoin(root, scheduledBackupKeysName)
	records, err := readScheduledBackupKeyRecords(store, path)
	if err != nil {
		return nil, err
	}
	if create && len(secret.Master) > 0 && secret.Record != nil {
		key, err := scheduledBackupKeyFromMaster(secret.Record.ID, secret.Master)
		if err != nil {
			return nil, err
		}
		index := -1
		for at, record := range records {
			if record.ID == secret.Record.ID {
				index = at
			}
		}
		if index < 0 || string(mustJSON(records[index])) != string(mustJSON(*secret.Record)) {
			if index < 0 {
				records = append(records, *secret.Record)
			} else {
				records[index] = *secret.Record
			}
			encoded, err := json.MarshalIndent(records, "", " ")
			if err != nil {
				return nil, err
			}
			if err := store.Replace(path, encoded); err != nil {
				return nil, err
			}
		}
		return key, nil
	}
	keys, err := scheduledBackupKeysIn(store, root, secret)
	if err != nil {
		return nil, err
	}
	if len(keys) > 0 {
		return keys[0], nil
	}
	if create && secret.Code != "" && len(secret.Master) == 0 {
		// A recovery code the environment gives: a backup key it wraps, and
		// the password too where one is given.
		master, record, err := newScheduledBackupMaster()
		if err != nil {
			return nil, err
		}
		for by, given := range map[string]string{scheduledBackupWrapCode: secret.Code, scheduledBackupWrapPassword: secret.Password} {
			if given == "" {
				continue
			}
			wrap, err := wrapScheduledBackupKey(master, record.ID, by, given)
			if err != nil {
				return nil, err
			}
			record.Wraps = append(record.Wraps, wrap)
		}
		return scheduledBackupKeyIn(store, root, scheduledBackupSecret{Master: master, Record: &record}, true)
	}
	if !create || secret.Password == "" || len(secret.Master) > 0 {
		return nil, errScheduledBackupPassword
	}
	salt := make([]byte, 16)
	id := make([]byte, 8)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	record := scheduledBackupKeyRecord{ID: hex.EncodeToString(id), Salt: hex.EncodeToString(salt), Iterations: scheduledBackupKeyIterations}
	key, err := deriveScheduledBackupKey(secret.Password, record)
	if err != nil {
		return nil, err
	}
	record.Check = key.check()
	encoded, err := json.MarshalIndent(append(records, record), "", " ")
	if err != nil {
		return nil, err
	}
	return key, store.Replace(path, encoded)
}

func mustJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}

func scheduledBackupNonce(prefix []byte, piece uint32, last bool) []byte {
	nonce := make([]byte, 12)
	copy(nonce, prefix)
	binary.BigEndian.PutUint32(nonce[7:11], piece)
	if last {
		nonce[11] = 1
	}
	return nonce
}

func (key *scheduledBackupKey) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(key.content)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// sealWriter seals what is written to it, piece by piece: a full piece waits
// until more follows, so the last one is known when the writer closes.
type sealWriter struct {
	output io.Writer
	aead   cipher.AEAD
	header []byte
	prefix []byte
	piece  []byte
	number uint32
}

// sealing starts a sealed file on output: the format, the key's ID and a
// random nonce prefix, then the pieces as they are written.
func (key *scheduledBackupKey) sealing(output io.Writer) (io.WriteCloser, error) {
	aead, err := key.aead()
	if err != nil {
		return nil, err
	}
	prefix := make([]byte, 7)
	if _, err := rand.Read(prefix); err != nil {
		return nil, err
	}
	id, err := hex.DecodeString(key.id)
	if err != nil || len(id) != 8 {
		return nil, errors.New("the backup key's ID is not valid")
	}
	header := append(append([]byte(scheduledBackupSealedMagic), id...), prefix...)
	if _, err := output.Write(header); err != nil {
		return nil, err
	}
	return &sealWriter{output: output, aead: aead, header: header, prefix: prefix,
		piece: make([]byte, 0, scheduledBackupSealedPiece)}, nil
}

func (writer *sealWriter) Write(data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		if len(writer.piece) == scheduledBackupSealedPiece {
			if err := writer.flush(false); err != nil {
				return written, err
			}
		}
		taken := min(len(data), scheduledBackupSealedPiece-len(writer.piece))
		writer.piece = append(writer.piece, data[:taken]...)
		data, written = data[taken:], written+taken
	}
	return written, nil
}

func (writer *sealWriter) flush(last bool) error {
	sealed := writer.aead.Seal(nil, scheduledBackupNonce(writer.prefix, writer.number, last), writer.piece, writer.header)
	writer.number++
	writer.piece = writer.piece[:0]
	_, err := writer.output.Write(sealed)
	return err
}

// Close seals the last piece; an empty file is one empty last piece.
func (writer *sealWriter) Close() error {
	return writer.flush(true)
}

// seal writes source to output, sealed.
func (key *scheduledBackupKey) seal(output io.Writer, source io.Reader) error {
	writer, err := key.sealing(output)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(writer, source)
	return errors.Join(copyErr, writer.Close())
}

// openSealed reads a sealed source back; its key must be this one.
func (key *scheduledBackupKey) openSealed(output io.Writer, source io.Reader) error {
	aead, err := key.aead()
	if err != nil {
		return err
	}
	header := make([]byte, len(scheduledBackupSealedMagic)+8+7)
	if _, err := io.ReadFull(source, header); err != nil || !bytes.HasPrefix(header, []byte(scheduledBackupSealedMagic)) {
		return errors.New("the backup file is not encrypted")
	}
	if hex.EncodeToString(header[len(scheduledBackupSealedMagic):len(scheduledBackupSealedMagic)+8]) != key.id {
		return errScheduledBackupPassword
	}
	prefix := header[len(scheduledBackupSealedMagic)+8:]
	sealed := make([]byte, scheduledBackupSealedPiece+aead.Overhead())
	// A source cut off midway, as a download whose connection dropped, says
	// so: read as a short last piece it would say the password is wrong.
	buffered := bufio.NewReaderSize(sealedSourceCut{source}, len(sealed))
	for number := uint32(0); ; number++ {
		read, err := io.ReadFull(buffered, sealed)
		if err != nil && (errors.Is(err, errSealedSourceCut) || !errors.Is(err, io.ErrUnexpectedEOF)) {
			return errors.Join(errors.New("the backup file ends before its last piece"), err)
		}
		// A short piece is the last; so is a full one nothing follows.
		last := read < len(sealed)
		if !last {
			if _, err := buffered.Peek(1); errors.Is(err, io.EOF) {
				last = true
			} else if err != nil {
				return err
			}
		}
		plain, err := aead.Open(nil, scheduledBackupNonce(prefix, number, last), sealed[:read], header)
		if err != nil {
			return errScheduledBackupPassword
		}
		if _, err := output.Write(plain); err != nil {
			return err
		}
		if last {
			return nil
		}
	}
}

// sealedSourceCut tells a source that ended before its end from the short
// last piece reading up to its end leaves (io.ReadFull's io.ErrUnexpectedEOF).
type sealedSourceCut struct{ source io.Reader }

var errSealedSourceCut = errors.New("the backup file was cut off before its end")

func (cut sealedSourceCut) Read(buffer []byte) (int, error) {
	read, err := cut.source.Read(buffer)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = errors.Join(errSealedSourceCut, err)
	}
	return read, err
}
