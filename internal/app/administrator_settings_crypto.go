package app

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	administratorSecretDirectory     = ".secrets"
	administratorSecretFilename      = "administrator-settings.key"
	administratorSecretRecoveryFile  = "administrator-settings.key.v0-recovery"
	administratorSecretFormatVersion = 1
)

var administratorSettingsSecretMutex sync.Mutex

type administratorSecretEnvelope struct {
	Version int    `json:"version"`
	Key     string `json:"key"`
}

func administratorSettingsSecret(app core.App) (string, error) {
	directory := filepath.Join(app.DataDir(), administratorSecretDirectory)
	path := filepath.Join(directory, administratorSecretFilename)
	return loadOrCreateVersionedSecretFile(versionedSecretFileConfig{
		Directory:        directory,
		Filename:         administratorSecretFilename,
		RecoveryFilename: administratorSecretRecoveryFile,
		FormatVersion:    administratorSecretFormatVersion,
		Mutex:            &administratorSettingsSecretMutex,
		Failure: func() error {
			return errors.New(errorCodeAdministratorSecretFailed)
		},
		BeforeCreate: func() error {
			return requireNoAdministratorSecretCiphertext(app)
		},
		RecoverInvalidCurrent: func() error {
			return recoverUnpublishedAdministratorSecret(app, directory, path)
		},
	})
}

func parseAdministratorSettingsSecret(source []byte) (string, bool, error) {
	return parseVersionedSecretFile(source, administratorSecretFileFormat())
}

func upgradeLegacyAdministratorSettingsSecret(
	directory, path, pendingPath, recoveryPath string,
	legacySource []byte,
	key string,
) (string, error) {
	config := administratorSecretFileFormat()
	config.Directory = directory
	return upgradeLegacyVersionedSecretFile(
		config, path, pendingPath, recoveryPath, legacySource, key,
	)
}

func administratorSecretFileFormat() versionedSecretFileConfig {
	return versionedSecretFileConfig{
		FormatVersion: administratorSecretFormatVersion,
		Failure: func() error {
			return errors.New(errorCodeAdministratorSecretFailed)
		},
	}
}

func recoverUnpublishedAdministratorSecret(app core.App, directory, path string) error {
	if err := requireNoAdministratorSecretCiphertext(app); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDirectory(directory)
}

func encryptAdministratorSetting(app core.App, purpose administratorCipherPurpose, value string) (string, error) {
	ciphers, err := encryptAdministratorSettings(app, purpose, []string{value})
	if err != nil {
		return "", err
	}
	return ciphers[0], nil
}

func encryptAdministratorSettings(app core.App, purpose administratorCipherPurpose, values []string) ([]string, error) {
	if _, registered := administratorCipherStorage[purpose]; !registered {
		return nil, errors.New(errorCodeAdministratorSecretFailed)
	}
	if len(values) == 0 {
		return nil, nil
	}
	key, err := administratorSettingsSecret(app)
	if err != nil {
		return nil, err
	}
	ciphers := make([]string, len(values))
	for index, value := range values {
		cipher, err := security.Encrypt([]byte(value), key)
		if err != nil {
			return nil, err
		}
		ciphers[index] = cipher
	}
	return ciphers, nil
}

func decryptAdministratorSetting(app core.App, cipher string) (string, error) {
	if cipher == "" {
		return "", nil
	}
	key, err := administratorSettingsSecret(app)
	if err != nil {
		return "", err
	}
	plain, err := security.Decrypt(cipher, key)
	return string(plain), err
}
