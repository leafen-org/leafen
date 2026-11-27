package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strings"
	"unicode"
)

// Backups and the replica that leave the machine are sealed (decided
// 2026-10-18, "모두 해줘, 백업은 비밀번호로도 복구 코드로도 할 수 있게"):
// each backup place's data is sealed with a random backup key, and that key
// is wrapped by a recovery code, a password, or both, kept in the place's
// keys.json beside it. Either opens the backups; changing or dropping one
// wraps the key again and seals nothing again. The server keeps the backup
// key itself, sealed with its administrator settings key, to go on writing.
//
// A recovery code is 20 random bytes in 32 letters and digits of Crockford's
// base32, in groups of four; it needs no stretching. A password is
// stretched with PBKDF2-SHA256 at 600,000 rounds, as before, and must be
// long and not a common one.

const (
	scheduledBackupWrapPassword = "password"
	scheduledBackupWrapCode     = "code"
	scheduledBackupCodeBytes    = 20
	scheduledBackupWrapDomain   = "leafen backup key wrap"
)

var (
	errScheduledBackupCodeInvalid  = errors.New("that isn't a recovery code")
	errScheduledBackupPasswordWeak = errors.New("the backup password is too weak")
	backupCodeEncoding             = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)
)

// scheduledBackupKeyWrap is the backup key wrapped by one way to open it.
type scheduledBackupKeyWrap struct {
	By         string `json:"by"`
	Salt       string `json:"salt"`
	Iterations int    `json:"iterations,omitempty"`
	Wrapped    string `json:"wrapped"`
}

// scheduledBackupSecret is what opens a backup place: the server's own
// backup key with its record, a recovery code or a password a person gives,
// and the password of an older place sealed by a password alone.
type scheduledBackupSecret struct {
	Master   []byte
	Record   *scheduledBackupKeyRecord
	Code     string
	Password string
	// Opened is a key taken from another server's place to read its backups:
	// this server writes nothing there until a person chooses it does.
	Opened bool
}

func (secret scheduledBackupSecret) empty() bool {
	return len(secret.Master) == 0 && secret.Code == "" && secret.Password == ""
}

func passwordBackupSecret(password string) scheduledBackupSecret {
	return scheduledBackupSecret{Password: password}
}

// newScheduledBackupCode is a fresh recovery code, as a person writes it.
func newScheduledBackupCode() (string, []byte, error) {
	raw := make([]byte, scheduledBackupCodeBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	letters := backupCodeEncoding.EncodeToString(raw)
	groups := make([]string, 0, len(letters)/4)
	for start := 0; start < len(letters); start += 4 {
		groups = append(groups, letters[start:start+4])
	}
	return strings.Join(groups, "-"), raw, nil
}

// decodeScheduledBackupCode reads a recovery code however it was written:
// any case, with or without dashes and spaces, O for 0 and I or L for 1, with
// a Korean keyboard on (typed_code_keys.go).
func decodeScheduledBackupCode(code string) ([]byte, error) {
	cleaned := strings.Map(func(r rune) rune {
		switch r = unicode.ToUpper(r); r {
		case '-', ' ', '\t':
			return -1
		case 'O':
			return '0'
		case 'I', 'L':
			return '1'
		}
		return r
	}, typedCodeKeys(code))
	raw, err := backupCodeEncoding.DecodeString(cleaned)
	if err != nil || len(raw) != scheduledBackupCodeBytes {
		return nil, errScheduledBackupCodeInvalid
	}
	return raw, nil
}

// wrapKey is the key one way wraps the backup key with.
func (wrap scheduledBackupKeyWrap) wrapKey(secret string) ([]byte, error) {
	salt, err := hex.DecodeString(wrap.Salt)
	if err != nil || len(salt) < 16 {
		return nil, errors.New("the backup's key record is not valid")
	}
	switch wrap.By {
	case scheduledBackupWrapPassword:
		if wrap.Iterations < 100_000 {
			return nil, errors.New("the backup's key record is not valid")
		}
		return pbkdf2.Key(sha256.New, secret, salt, wrap.Iterations, 32)
	case scheduledBackupWrapCode:
		raw, err := decodeScheduledBackupCode(secret)
		if err != nil {
			return nil, err
		}
		return hkdf.Key(sha256.New, raw, salt, scheduledBackupWrapDomain, 32)
	}
	return nil, errors.New("the backup's key record is not valid")
}

func scheduledBackupWrapAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// wrapScheduledBackupKey wraps the backup key of the record id by a code or
// a password.
func wrapScheduledBackupKey(master []byte, id, by, secret string) (scheduledBackupKeyWrap, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return scheduledBackupKeyWrap{}, err
	}
	wrap := scheduledBackupKeyWrap{By: by, Salt: hex.EncodeToString(salt)}
	if by == scheduledBackupWrapPassword {
		wrap.Iterations = scheduledBackupKeyIterations
	}
	key, err := wrap.wrapKey(secret)
	if err != nil {
		return scheduledBackupKeyWrap{}, err
	}
	aead, err := scheduledBackupWrapAEAD(key)
	if err != nil {
		return scheduledBackupKeyWrap{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return scheduledBackupKeyWrap{}, err
	}
	sealed := aead.Seal(nonce, nonce, master, []byte(scheduledBackupWrapDomain+"\x00"+id+"\x00"+by))
	wrap.Wrapped = hex.EncodeToString(sealed)
	return wrap, nil
}

// unwrap opens the backup key the wrap holds with a code or a password.
func (wrap scheduledBackupKeyWrap) unwrap(id, secret string) ([]byte, error) {
	key, err := wrap.wrapKey(secret)
	if err != nil {
		return nil, err
	}
	aead, err := scheduledBackupWrapAEAD(key)
	if err != nil {
		return nil, err
	}
	sealed, err := hex.DecodeString(wrap.Wrapped)
	if err != nil || len(sealed) < aead.NonceSize() {
		return nil, errScheduledBackupPassword
	}
	master, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(scheduledBackupWrapDomain+"\x00"+id+"\x00"+wrap.By))
	if err != nil {
		return nil, errScheduledBackupPassword
	}
	return master, nil
}

// scheduledBackupKeyFromMaster is the content and name keys a backup key
// makes, as a password's stretched key made them before.
func scheduledBackupKeyFromMaster(id string, master []byte) (*scheduledBackupKey, error) {
	content, err := hkdf.Key(sha256.New, master, nil, "leafen backup content", 32)
	if err != nil {
		return nil, err
	}
	names, err := hkdf.Key(sha256.New, master, nil, "leafen backup names", 32)
	if err != nil {
		return nil, err
	}
	return &scheduledBackupKey{id: id, content: content, names: names}, nil
}

// newScheduledBackupMaster is a fresh backup key and its record, wrapped by
// nothing yet.
func newScheduledBackupMaster() ([]byte, scheduledBackupKeyRecord, error) {
	master := make([]byte, 32)
	id := make([]byte, 8)
	if _, err := rand.Read(master); err != nil {
		return nil, scheduledBackupKeyRecord{}, err
	}
	if _, err := rand.Read(id); err != nil {
		return nil, scheduledBackupKeyRecord{}, err
	}
	record := scheduledBackupKeyRecord{ID: hex.EncodeToString(id), Wraps: []scheduledBackupKeyWrap{}}
	key, err := scheduledBackupKeyFromMaster(record.ID, master)
	if err != nil {
		return nil, scheduledBackupKeyRecord{}, err
	}
	record.Check = key.check()
	return master, record, nil
}

// openScheduledBackupRecord opens a record of a place with the secret: an
// older record, made from a password alone, by its password; a wrapped one
// by the server's backup key, or by any of its wraps the code or the
// password opens.
func openScheduledBackupRecord(record scheduledBackupKeyRecord, secret scheduledBackupSecret) (*scheduledBackupKey, []byte) {
	checked := func(key *scheduledBackupKey, err error) *scheduledBackupKey {
		if err == nil && key != nil && hmac.Equal([]byte(key.check()), []byte(record.Check)) {
			return key
		}
		return nil
	}
	if record.Wraps == nil {
		if secret.Password == "" {
			return nil, nil
		}
		return checked(deriveScheduledBackupKey(secret.Password, record)), nil
	}
	if len(secret.Master) > 0 && (secret.Record == nil || secret.Record.ID == record.ID) {
		if key := checked(scheduledBackupKeyFromMaster(record.ID, secret.Master)); key != nil {
			return key, secret.Master
		}
	}
	for _, wrap := range record.Wraps {
		given := secret.Password
		if wrap.By == scheduledBackupWrapCode {
			given = secret.Code
		}
		if given == "" {
			continue
		}
		master, err := wrap.unwrap(record.ID, given)
		if err != nil {
			continue
		}
		if key := checked(scheduledBackupKeyFromMaster(record.ID, master)); key != nil {
			return key, master
		}
	}
	return nil, nil
}

// checkBackupPassword refuses a password a guessing machine would find:
// short, one letter again and again, a run of the keyboard or the alphabet,
// or one of the passwords people use most.
func checkBackupPassword(password string) error {
	runes := []rune(password)
	if len(runes) < minScheduledBackupPasswordLength {
		return errScheduledBackupPasswordWeak
	}
	lower := strings.ToLower(password)
	distinct := map[rune]bool{}
	for _, r := range lower {
		distinct[r] = true
	}
	if len(distinct) < 4 {
		return errScheduledBackupPasswordWeak
	}
	for _, run := range []string{"abcdefghijklmnopqrstuvwxyz", "0123456789012345678901234567890", "qwertyuiopasdfghjklzxcvbnm", "1q2w3e4r5t6y7u8i9o0p"} {
		if strings.Contains(run, lower) {
			return errScheduledBackupPasswordWeak
		}
	}
	collapsed := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			return r
		}
		return -1
	}, lower)
	for _, common := range commonBackupPasswords {
		if lower == common || collapsed == common || strings.HasPrefix(lower, common) && len(distinct) < 8 {
			return errScheduledBackupPasswordWeak
		}
	}
	return nil
}

// commonBackupPasswords are passwords found at the top of every leaked list,
// and their padded forms.
var commonBackupPasswords = []string{
	"password", "passw0rd", "iloveyou", "qwerty", "abc123", "letmein", "welcome", "monkey", "dragon",
	"football", "baseball", "sunshine", "princess", "admin", "administrator", "leafen", "backup", "secret",
	"changeme", "trustno1", "superman", "master", "shadow", "michael", "qwertyuiop", "asdfghjkl",
}
