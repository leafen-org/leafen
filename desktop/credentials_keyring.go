//go:build windows

package main

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// keyringStore keeps sign-ins in Windows' Credential Manager.
type keyringStore struct{}

func platformSecretStore() secretStore { return keyringStore{} }

func (keyringStore) load(account string) (string, bool, error) {
	secret, err := keyring.Get(credentialService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return secret, true, nil
}

func (keyringStore) save(account, secret string) error {
	return keyring.Set(credentialService, account, secret)
}

func (keyringStore) forget(account string) error {
	if err := keyring.Delete(credentialService, account); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}
