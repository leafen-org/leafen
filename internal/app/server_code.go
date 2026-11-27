package app

import (
	"crypto/sha256"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A server's code (user decision 2026-11-23: an agent connected to more than
// one Leafen knows from the code itself which one to approve it on): three
// letters from the server's own number (server_identity.go), so a server
// keeps it through backups, restores and moves and two servers rarely share
// one. The codes an agent approves for its person — a browser's sign-in
// (agent_sign_in_requests.go), another agent's connection
// (mcp_device_connections.go) — begin with it ("TUL·KX7-4QP"), each
// connection's instructions name it (mcp_session_instructions.go), and a code
// of another server's is answered with which one it is. A code typed without
// it, as screens showed them before and a phone's number pad types them, is
// read as before.

const (
	// serverCodeAlphabet is consonants alone: no code reads as a word, and
	// none holds I, L or O, which a typed code reads as digits (readTypedCode).
	serverCodeAlphabet  = "BCDFGHJKMNPQRSTVWXZ"
	serverCodeLength    = 3
	serverCodeSeparator = "·"
)

// serverCodeFor is the code a server's own number gives.
func serverCodeFor(serverID string) string {
	digest := sha256.Sum256([]byte(serverID))
	code := make([]byte, serverCodeLength)
	for index := range code {
		code[index] = serverCodeAlphabet[int(digest[index])%len(serverCodeAlphabet)]
	}
	return string(code)
}

// loadServerCode is this server's code.
func loadServerCode(app core.App) (string, error) {
	serverID, err := loadServerID(app)
	if err != nil {
		return "", err
	}
	return serverCodeFor(serverID), nil
}

// codeWithServerCode is a code as a screen shows it: this server's code
// before it; the code alone when the server's can't be read.
func codeWithServerCode(app core.App, shown string) string {
	server, err := loadServerCode(app)
	if err != nil {
		return shown
	}
	return server + serverCodeSeparator + shown
}

// splitServerCode reads a code as typed, said or pasted (readTypedCode), of
// length letters after the server's code, if it has one: the code without
// it, and the server's code it began with, "" for none.
func splitServerCode(typed string, length int) (code, server string) {
	read := readTypedCode(typed)
	if len(read) == length+serverCodeLength && strings.Trim(read[:serverCodeLength], serverCodeAlphabet) == "" {
		return read[serverCodeLength:], read[:serverCodeLength]
	}
	return read, ""
}

// otherServerCode is the server's code a code typed for another server
// began with, "" for one of this server's or one without any.
func otherServerCode(app core.App, typed string, length int) string {
	_, server := splitServerCode(typed, length)
	if server == "" {
		return ""
	}
	if own, err := loadServerCode(app); err == nil && server == own {
		return ""
	}
	return server
}

// otherServerCodeNote tells an agent a code is another Leafen's, and where
// it is approved.
func otherServerCodeNote(app core.App, server, what string) string {
	own, _ := loadServerCode(app)
	return "This " + what + " code is another Leafen's: it begins with server code " + server + ", and this one's is " + own +
		". Approve it on your connection to the Leafen whose instructions name server code " + server + "; nothing was done here."
}
