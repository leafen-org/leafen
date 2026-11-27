package app

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const (
	setupOpenModeEnvironment = "LEAFEN_SETUP_OPEN_MODE"
	setupOpenModeBackground  = "background"
	setupOpenModePrint       = "print"
	errorCodeSetupToken      = "LEAFEN_SETUP_TOKEN_FAILED"
)

func nativeInstaller(configuredMode string) func(core.App, *core.Record, string) error {
	mode := normalizedSetupOpenMode(configuredMode)

	return func(app core.App, _ *core.Record, baseURL string) error {
		configured, err := administratorConfigured(app)
		if err != nil {
			return fmt.Errorf("%s: %w", errorCodeSetupToken, err)
		}
		if configured {
			return nil
		}

		setupURL := fmt.Sprintf(
			"%s/setup%s",
			strings.TrimRight(setupBaseURL(baseURL), "/"), setupLinkFragment(app),
		)
		opened := mode == setupOpenModeBackground && openSetupURLWithoutFocus(setupURL)

		if opened {
			fmt.Println("\nLeafen first-run setup opened in the background.")
		} else {
			fmt.Println("\nOpen the Leafen first-run setup URL in a browser:")
		}
		fmt.Println(setupURL)
		return nil
	}
}

// publishedPortEnvironment is the host side of a container's port mapping
// ("8091" or "127.0.0.1:8091"), which compose.yaml and run-self-hosted.sh
// pass in: the setup address is printed there, not at the port inside.
const publishedPortEnvironment = "LEAFEN_PUBLISHED_PORT"

// setupBaseURL is the address a person opens to set the server up: the
// public address the operator configured, as behind a reverse proxy or a
// NAS's port mapping; otherwise the address it serves on, as this machine
// opens it.
func setupBaseURL(serveURL string) string {
	if origin := configuredPublicOrigin(); origin != "" {
		return origin
	}
	return thisMachineURL(serveURL)
}

// thisMachineURL is the address the server serves on as this machine opens
// it: a server listening on every interface (0.0.0.0, as the Docker image
// does) is reached at 127.0.0.1, at the port it is published on when a
// container says so. The setup address, `leafen setup status`, the MCP
// address for agents here and device codes' links all take it, so they say
// the same port (2026-11-17: with LEAFEN_PORT=19030 the log said 19030 and
// setup status 8090, the port inside the container).
func thisMachineURL(serveURL string) string {
	parsed, err := url.Parse(serveURL)
	if err != nil || parsed.Host == "" {
		return serveURL
	}
	if ip := net.ParseIP(parsed.Hostname()); parsed.Hostname() == "" || (ip != nil && ip.IsUnspecified()) {
		host, port := "127.0.0.1", parsed.Port()
		if published := strings.TrimSpace(os.Getenv(publishedPortEnvironment)); published != "" {
			if mappedHost, mappedPort, splitErr := net.SplitHostPort(published); splitErr == nil {
				if mapped := net.ParseIP(mappedHost); mapped != nil && !mapped.IsUnspecified() {
					host = mappedHost
				}
				port = mappedPort
			} else if _, numberErr := strconv.Atoi(published); numberErr == nil {
				port = published
			}
		}
		if port != "" {
			parsed.Host = net.JoinHostPort(host, port)
		} else {
			parsed.Host = host
		}
	}
	return parsed.String()
}

func normalizedSetupOpenMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case setupOpenModeBackground:
		return setupOpenModeBackground
	case setupOpenModePrint:
		return setupOpenModePrint
	default:
		return setupOpenModePrint
	}
}

func openSetupURLWithoutFocus(setupURL string) bool {
	// macOS `open -g` was observed activating the default browser, and the
	// other supported platforms expose no portable app/window/tab-preserving
	// primitive. Keep the capability fail-closed until a platform-specific
	// implementation passes the exact focus guard.
	_ = setupURL
	return false
}
