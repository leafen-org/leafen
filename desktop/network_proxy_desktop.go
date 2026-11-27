//go:build (darwin && !ios) || windows

package main

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"leafen.org/leafen/internal/netproxy"
)

// NetworkProxy is the proxy this computer's Leafen goes out by — the app and
// its own server: the system's settings and the environment unless the
// person sets one in This computer, its password kept in the computer's
// secret storage.
type NetworkProxy struct {
	mu sync.Mutex
}

// NetworkProxyState is the setting as the window shows it, never the
// password, with the proxy in force.
type NetworkProxyState struct {
	Mode        string         `json:"mode"`
	URL         string         `json:"url"`
	NoProxy     string         `json:"noProxy"`
	Username    string         `json:"username"`
	PasswordSet bool           `json:"passwordSet"`
	InForce     netproxy.State `json:"inForce"`
}

// NetworkProxyInput is a change from the window; a password left empty
// keeps the one kept.
type NetworkProxyInput struct {
	Mode          string `json:"mode"`
	URL           string `json:"url"`
	NoProxy       string `json:"noProxy"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	ClearPassword bool   `json:"clearPassword"`
}

type networkProxyFile struct {
	Mode     string `json:"mode"`
	URL      string `json:"url"`
	NoProxy  string `json:"noProxy"`
	Username string `json:"username"`
}

const networkProxyAccount = "network-proxy"

var errNetworkProxy = errors.New("check the proxy: an http:// or https:// address with its port")

// networkProxyPath is beside the app's data folder, as its first start's
// choices are.
func networkProxyPath() (string, error) {
	dir, err := localDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(dir), "network-proxy.json"), nil
}

func readNetworkProxy() networkProxyFile {
	kept := networkProxyFile{Mode: "system"}
	if path, err := networkProxyPath(); err == nil {
		if raw, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(raw, &kept)
		}
	}
	if kept.Mode != "manual" {
		kept.Mode = "system"
	}
	return kept
}

// applyAppProxy makes the person's proxy the app's, at its start and as it
// changes.
func applyAppProxy() error {
	kept := readNetworkProxy()
	if kept.Mode != "manual" || kept.URL == "" {
		return netproxy.Set(nil)
	}
	proxy, err := url.Parse(kept.URL)
	if err != nil {
		return err
	}
	if kept.Username != "" {
		password := ""
		if store := platformSecretStore(); store != nil {
			password, _, _ = store.load(networkProxyAccount)
		}
		proxy.User = url.UserPassword(kept.Username, password)
	}
	return netproxy.Set(&netproxy.Config{URL: proxy.String(), NoProxy: kept.NoProxy})
}

func (proxy *NetworkProxy) state(kept networkProxyFile) NetworkProxyState {
	passwordSet := false
	if store := platformSecretStore(); store != nil && kept.Username != "" {
		_, passwordSet, _ = store.load(networkProxyAccount)
	}
	return NetworkProxyState{Mode: kept.Mode, URL: kept.URL, NoProxy: kept.NoProxy, Username: kept.Username,
		PasswordSet: passwordSet, InForce: netproxy.Current()}
}

// State is the setting and the proxy in force.
func (proxy *NetworkProxy) State() NetworkProxyState {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	return proxy.state(readNetworkProxy())
}

// Set keeps the person's choice and goes by it at once.
func (proxy *NetworkProxy) Set(input NetworkProxyInput) (NetworkProxyState, error) {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	next := networkProxyFile{Mode: "system"}
	if input.Mode == "manual" {
		parsed, err := netproxy.Check(netproxy.Config{URL: input.URL})
		if err != nil || parsed.User != nil || len(input.NoProxy) > 2048 || len(input.Username) > 256 || len(input.Password) > 4096 ||
			strings.ContainsAny(input.NoProxy+input.Username, "\r\n") {
			return NetworkProxyState{}, errNetworkProxy
		}
		next = networkProxyFile{Mode: "manual", URL: parsed.String(), NoProxy: strings.TrimSpace(input.NoProxy), Username: strings.TrimSpace(input.Username)}
	}
	store := platformSecretStore()
	switch {
	case next.Username == "" || input.ClearPassword:
		if store != nil {
			_ = store.forget(networkProxyAccount)
		}
	case input.Password != "":
		if store == nil {
			return NetworkProxyState{}, errCredentialStore
		}
		if err := store.save(networkProxyAccount, input.Password); err != nil {
			return NetworkProxyState{}, err
		}
	}
	path, err := networkProxyPath()
	if err != nil {
		return NetworkProxyState{}, err
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return NetworkProxyState{}, err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return NetworkProxyState{}, err
	}
	if err := applyAppProxy(); err != nil {
		return NetworkProxyState{}, err
	}
	return proxy.state(next), nil
}
