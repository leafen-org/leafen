package app

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// A packaged Leafen app signs in to another server through its providers in
// the system browser, as RFC 8252 has native apps do. It names where the
// browser brings the sign-in back — a loopback address it listens on for that
// sign-in, or its own URL scheme — and a PKCE challenge (S256). The server
// sends the completion code there instead of to its own page, and takes that
// code only with the verifier the challenge was made from, so a code caught
// on the way is no use.

const (
	authAppScheme         = "org.leafen.app"
	maxAuthAppReturnBytes = 512
	authAppReturnParam    = "app_return"
	authAppChallengeParam = "app_challenge"
)

var (
	authAppChallengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	authAppVerifierPattern  = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
)

// authAppReturn is where a sign-in started by a packaged app goes back to and
// the challenge its completion must answer; both are empty for a sign-in that
// comes back to the server's own page.
type authAppReturn struct {
	Address   string
	Challenge string
}

// parseAuthAppReturn reads a sign-in start's app return and challenge,
// reporting false when either is given without the other or isn't valid.
func parseAuthAppReturn(address, challenge string) (authAppReturn, bool) {
	address, challenge = strings.TrimSpace(address), strings.TrimSpace(challenge)
	if address == "" && challenge == "" {
		return authAppReturn{}, true
	}
	if !validAuthAppReturnAddress(address) || !authAppChallengePattern.MatchString(challenge) {
		return authAppReturn{}, false
	}
	return authAppReturn{Address: address, Challenge: challenge}, true
}

// validAuthAppReturnAddress accepts a loopback address by its IP literal
// with a port (RFC 8252 7.3) or the app's own scheme with no host (7.1).
func validAuthAppReturnAddress(value string) bool {
	if value == "" || len(value) > maxAuthAppReturnBytes || strings.ContainsAny(value, "\\\x00\r\n\t ") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" ||
		!strings.HasPrefix(parsed.Path, "/") {
		return false
	}
	switch parsed.Scheme {
	case "http":
		host, port, err := net.SplitHostPort(parsed.Host)
		if err != nil {
			return false
		}
		number, err := strconv.Atoi(port)
		return err == nil && number >= 1 && number <= 65535 && (host == "127.0.0.1" || host == "::1")
	case authAppScheme:
		return parsed.Host == ""
	}
	return false
}

// authAppVerifierMatches reports whether a PKCE verifier makes a challenge.
func authAppVerifierMatches(challenge, verifier string) bool {
	if !authAppVerifierPattern.MatchString(verifier) {
		return false
	}
	digest := sha256.Sum256([]byte(verifier))
	made := base64.RawURLEncoding.EncodeToString(digest[:])
	return subtle.ConstantTimeCompare([]byte(made), []byte(challenge)) == 1
}

// authAppReturnDestination is the app's return address carrying a code.
func authAppReturnDestination(address, name, code string) (string, error) {
	destination, err := url.Parse(address)
	if err != nil {
		return "", err
	}
	query := destination.Query()
	query.Set(name, code)
	destination.RawQuery = query.Encode()
	return destination.String(), nil
}
