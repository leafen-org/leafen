package app

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type versionedSecretFileConfig struct {
	Directory             string
	Filename              string
	RecoveryFilename      string
	FormatVersion         int
	Mutex                 *sync.Mutex
	Failure               func() error
	BeforeCreate          func() error
	RecoverInvalidCurrent func() error
}

type versionedSecretFileEnvelope struct {
	Version int    `json:"version"`
	Key     string `json:"key"`
}

func loadOrCreateVersionedSecretFile(config versionedSecretFileConfig) (string, error) {
	config.Mutex.Lock()
	defer config.Mutex.Unlock()

	if err := os.MkdirAll(config.Directory, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(config.Directory, config.Filename)
	pendingPath := path + ".pending"
	recoveryPath := filepath.Join(config.Directory, config.RecoveryFilename)
	currentRecovered := false
	if existing, err := os.ReadFile(path); err == nil {
		key, legacy, parseErr := parseVersionedSecretFile(existing, config)
		if parseErr == nil {
			if legacy {
				return upgradeLegacyVersionedSecretFile(
					config, path, pendingPath, recoveryPath, existing, key,
				)
			}
			if err := finishVersionedSecretFileMigration(
				config, pendingPath, recoveryPath, key,
			); err != nil {
				return "", err
			}
			return key, nil
		}
		if looksLikeVersionedSecretFile(existing) {
			return "", config.Failure()
		}
		if recoveredKey, recoveryErr := readLegacyVersionedSecretRecovery(recoveryPath, config); recoveryErr == nil {
			return upgradeLegacyVersionedSecretFile(
				config, path, pendingPath, recoveryPath, []byte(recoveredKey+"\n"), recoveredKey,
			)
		} else if !errors.Is(recoveryErr, os.ErrNotExist) {
			return "", recoveryErr
		}
		if config.RecoverInvalidCurrent == nil {
			return "", config.Failure()
		}
		if err := config.RecoverInvalidCurrent(); err != nil {
			return "", err
		}
		currentRecovered = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	} else {
		if recoveredKey, recoveryErr := readLegacyVersionedSecretRecovery(recoveryPath, config); recoveryErr == nil {
			return upgradeLegacyVersionedSecretFile(
				config, path, pendingPath, recoveryPath, []byte(recoveredKey+"\n"), recoveredKey,
			)
		} else if !errors.Is(recoveryErr, os.ErrNotExist) {
			return "", recoveryErr
		}
	}

	if pending, err := os.ReadFile(pendingPath); err == nil {
		key, legacy, parseErr := parseVersionedSecretFile(pending, config)
		if parseErr == nil {
			if legacy {
				if err := removeVersionedSecretFiles(config.Directory, pendingPath); err != nil {
					return "", err
				}
				return createVersionedSecretFile(config, pendingPath, path, key)
			}
			if err := syncPath(pendingPath); err != nil {
				return "", err
			}
			return publishVersionedSecretFile(config, pendingPath, path, key)
		}
		if looksLikeVersionedSecretFile(pending) {
			return "", config.Failure()
		}
		if err := removeVersionedSecretFiles(config.Directory, pendingPath); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if !currentRecovered && config.BeforeCreate != nil {
		if err := config.BeforeCreate(); err != nil {
			return "", err
		}
	}
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return createVersionedSecretFile(
		config, pendingPath, path, base64.RawURLEncoding.EncodeToString(random),
	)
}

func parseVersionedSecretFile(
	source []byte,
	config versionedSecretFileConfig,
) (string, bool, error) {
	raw := strings.TrimSpace(string(source))
	if validVersionedSecretKey(raw) {
		return raw, true, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.DisallowUnknownFields()
	envelope := versionedSecretFileEnvelope{}
	if err := decoder.Decode(&envelope); err != nil {
		return "", false, config.Failure()
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return "", false, config.Failure()
	}
	if envelope.Version != config.FormatVersion || !validVersionedSecretKey(envelope.Key) {
		return "", false, config.Failure()
	}
	return envelope.Key, false, nil
}

func encodeVersionedSecretFile(key string, config versionedSecretFileConfig) ([]byte, error) {
	if !validVersionedSecretKey(key) {
		return nil, config.Failure()
	}
	source, err := json.Marshal(versionedSecretFileEnvelope{
		Version: config.FormatVersion,
		Key:     key,
	})
	if err != nil {
		return nil, err
	}
	return append(source, '\n'), nil
}

func validVersionedSecretKey(key string) bool {
	if len(key) != 32 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(key)
	return err == nil && len(decoded) == 24
}

func looksLikeVersionedSecretFile(source []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(source), []byte("{"))
}

func readLegacyVersionedSecretRecovery(
	recoveryPath string,
	config versionedSecretFileConfig,
) (string, error) {
	source, err := os.ReadFile(recoveryPath)
	if err != nil {
		return "", err
	}
	key, legacy, err := parseVersionedSecretFile(source, config)
	if err != nil || !legacy {
		return "", config.Failure()
	}
	return key, nil
}

func upgradeLegacyVersionedSecretFile(
	config versionedSecretFileConfig,
	path, pendingPath, recoveryPath string,
	legacySource []byte,
	key string,
) (string, error) {
	if recoveredKey, err := readLegacyVersionedSecretRecovery(recoveryPath, config); err == nil {
		if recoveredKey != key {
			return "", config.Failure()
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	} else {
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		if err := os.Link(path, recoveryPath); err != nil {
			// Where links are refused the recovery is a copy, checked below.
			if !linkRefused(err) {
				return "", err
			}
			if err := writeVersionedSecretFile(recoveryPath, legacySource); err != nil {
				return "", err
			}
		}
		if err := errors.Join(syncPath(recoveryPath), syncDirectory(config.Directory)); err != nil {
			return "", err
		}
		stored, err := os.ReadFile(recoveryPath)
		if err != nil || !bytes.Equal(stored, legacySource) {
			return "", config.Failure()
		}
	}

	if pending, err := os.ReadFile(pendingPath); err == nil {
		pendingKey, pendingLegacy, parseErr := parseVersionedSecretFile(pending, config)
		if parseErr == nil && pendingKey != key {
			return "", config.Failure()
		}
		if parseErr != nil || pendingLegacy {
			if parseErr != nil && json.Valid(pending) && looksLikeVersionedSecretFile(pending) {
				return "", config.Failure()
			}
			if err := removeVersionedSecretFiles(config.Directory, pendingPath); err != nil {
				return "", err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if _, err := os.Stat(pendingPath); errors.Is(err, os.ErrNotExist) {
		source, encodeErr := encodeVersionedSecretFile(key, config)
		if encodeErr != nil {
			return "", encodeErr
		}
		if writeErr := writeVersionedSecretFile(pendingPath, source); writeErr != nil {
			return "", writeErr
		}
	} else if err != nil {
		return "", err
	} else if err := syncPath(pendingPath); err != nil {
		return "", err
	}
	if err := os.Rename(pendingPath, path); err != nil {
		return "", err
	}
	if err := syncDirectory(config.Directory); err != nil {
		return "", err
	}
	if err := removeVersionedSecretFiles(config.Directory, recoveryPath); err != nil {
		return "", err
	}
	return key, nil
}

func createVersionedSecretFile(
	config versionedSecretFileConfig,
	pendingPath, path, key string,
) (string, error) {
	source, err := encodeVersionedSecretFile(key, config)
	if err != nil {
		return "", err
	}
	if err := writeVersionedSecretFile(pendingPath, source); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		pending, readErr := os.ReadFile(pendingPath)
		pendingKey, legacy, parseErr := parseVersionedSecretFile(pending, config)
		if readErr != nil || parseErr != nil || legacy {
			return "", config.Failure()
		}
		key = pendingKey
	}
	return publishVersionedSecretFile(config, pendingPath, path, key)
}

func writeVersionedSecretFile(path string, source []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(source); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func publishVersionedSecretFile(
	config versionedSecretFileConfig,
	pendingPath, path, key string,
) (string, error) {
	if err := publishNoReplace(pendingPath, path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
		existing, readErr := os.ReadFile(path)
		existingKey, legacy, parseErr := parseVersionedSecretFile(existing, config)
		if readErr != nil || parseErr != nil || legacy {
			return "", config.Failure()
		}
		key = existingKey
	}
	if err := syncDirectory(config.Directory); err != nil {
		return "", err
	}
	if err := finishVersionedSecretFileMigration(config, pendingPath, "", key); err != nil {
		return "", err
	}
	return key, nil
}

func finishVersionedSecretFileMigration(
	config versionedSecretFileConfig,
	pendingPath, recoveryPath, key string,
) error {
	for _, residuePath := range []string{pendingPath, recoveryPath} {
		if residuePath == "" {
			continue
		}
		source, err := os.ReadFile(residuePath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		residueKey, _, parseErr := parseVersionedSecretFile(source, config)
		if parseErr != nil || residueKey != key {
			return config.Failure()
		}
	}
	return removeVersionedSecretFiles(config.Directory, pendingPath, recoveryPath)
}

func removeVersionedSecretFiles(directory string, paths ...string) error {
	changed := false
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		changed = true
	}
	if changed {
		return syncDirectory(directory)
	}
	return nil
}
