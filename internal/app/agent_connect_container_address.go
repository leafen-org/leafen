package app

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The address `leafen connect` names for the server. On this computer it is
// the one the running server last said it is at (agentConnectOrigin). In a
// container that address is the host's only when the container was told the
// port its host publishes it on (LEAFEN_PUBLISHED_PORT, which compose.yaml
// passes, thisMachineURL); otherwise it is the container's own,
// http://127.0.0.1:8090, which neither the host nor another computer reaches
// (2026-11-23, a NAS publishing its container at 127.0.0.1:49163 behind a
// public address: the Claude Code entry `--claude-code-json` gave named the
// inside address, and it was mended by hand). There every output of the
// command — Claude Code's entry, another program's MCP address, the line the
// question step gives and the check of an entry held already — names the
// address given with --url, else the server's public one
// (configuredPublicOrigin: the way out chosen in the settings, else
// LEAFEN_PUBLIC_URL), else the outside address the server last said; with
// none, the command says in one line how to give it, and makes nothing.

// agentConnectInContainer says whether the command runs in a container: its
// marks, or the image's LEAFEN_CONTAINER_RUNTIME, as the backup and update
// ways judge it.
func agentConnectInContainer() bool {
	return runningInContainer() || strings.TrimSpace(os.Getenv(containerRuntimeEnvironment)) == "1"
}

// agentConnectAddress is that address, from --url (given) or as above; ""
// with no error when the server isn't running here.
func agentConnectAddress(app core.App, given string) (string, error) {
	if given = strings.TrimSpace(given); given != "" {
		parsed, err := url.Parse(given)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return "", fmt.Errorf("--url is the address Leafen is opened at, such as http://127.0.0.1:8090 or https://leafen.example.com")
		}
		return parsed.Scheme + "://" + parsed.Host, nil
	}
	noteCommandTunnelChoice(app)
	if !agentConnectInContainer() || strings.TrimSpace(os.Getenv(publishedPortEnvironment)) != "" {
		return agentConnectOrigin(app), nil
	}
	if public := strings.TrimRight(configuredPublicOrigin(), "/"); public != "" {
		return public, nil
	}
	facts := readSetupState(app.DataDir()).Facts
	if outside := strings.TrimRight(facts.Outside, "/"); outside != "" {
		return outside, nil
	}
	if strings.TrimSpace(facts.Here) == "" {
		return "", nil
	}
	inside := "8090"
	if parsed, err := url.Parse(facts.Here); err == nil {
		if _, port, splitErr := net.SplitHostPort(parsed.Host); splitErr == nil {
			inside = port
		}
	}
	return "", fmt.Errorf("Leafen runs in a container here, which can't tell the address and port its host publishes it at, and it has "+
		"no public address: run this again with --url and the address Leafen is opened at, such as --url http://127.0.0.1:<the port "+
		"`docker compose port leafen %s` prints on the host> (or give the container LEAFEN_PUBLISHED_PORT or LEAFEN_PUBLIC_URL)", inside)
}

// agentConnectReachAt is the reach a plan's entry held already is checked
// against in a container: at the address the command names, and the
// server's outside ones — not the container's own, which nothing outside
// it reaches.
func agentConnectReachAt(app core.App, plan agentConnectPlan) agentEntryReach {
	if !agentConnectInContainer() || plan.Origin == "" {
		return plan.Reach
	}
	addresses := []string{plan.Origin + nativeMCPPath}
	for _, origin := range []string{readSetupState(app.DataDir()).Facts.Outside, configuredPublicOrigin()} {
		if origin = strings.TrimRight(origin, "/"); origin != "" {
			addresses = append(addresses, origin+nativeMCPPath)
		}
	}
	return agentEntryReachFor(addresses, plan.OwnerID)
}
