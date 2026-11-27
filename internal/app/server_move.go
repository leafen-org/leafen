package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Moving a whole server to another (the agent decision of 2026-10-19 09:31):
// the new server, empty but for its administrator, opens a place to receive
// for an hour and gives a code; the old server, given it, stops taking
// changes, lets go of its address and writes a snapshot — the scheduled
// backups' own, sealed with the code's secret — straight into that place
// over WebDAV; the new server checks every file against the snapshot's list
// and restores it as a restore from Settings does, starting again with the
// old server's people, history, files, settings and Tailscale device. The
// old server stays, reading only, pointing to the new one (the replica's
// "moved" fence), until its administrator keeps it apart. Leafen copies;
// an agent that guides the move never carries the content.

const (
	serverMoveCodePrefix = "leafen-move-1:"
	serverMoveCodeFor    = time.Hour
	// serverMovedInName is the new server's note of what it took in, kept
	// through the restore beside the data as a machine's own files are.
	serverMovedInName = ".leafen-moved-in"
	serverMoveInDir   = ".leafen-move-in"
	// How long the old server waits after stopping changes, for the
	// changes already on their way to land before the snapshot.
	serverMoveSettle = 2 * time.Second
)

var serverMoveIDPattern = regexp.MustCompile(`^[a-z0-9]{20}$`)

// serverMoveCode is what the new server's code carries: where to send, the
// receiving place, and the secret that opens it and seals the snapshot.
type serverMoveCode struct {
	To      string `json:"to"`
	Also    string `json:"also,omitempty"`
	ID      string `json:"id"`
	Secret  string `json:"secret"`
	Release string `json:"release"`
	Expires int64  `json:"expires"`
}

func encodeServerMoveCode(code serverMoveCode) string {
	return encodeMoveCode(serverMoveCodePrefix, code)
}

func decodeServerMoveCode(raw string, now time.Time) (serverMoveCode, error) {
	return decodeMoveCode(serverMoveCodePrefix, raw, now)
}

// encodeMoveCode and decodeMoveCode are a move's code, a whole server's or a
// workspace's (workspace_move_in.go), by its prefix.
func encodeMoveCode(prefix string, code serverMoveCode) string {
	raw, _ := json.Marshal(code)
	return prefix + base64.RawURLEncoding.EncodeToString(raw)
}

func decodeMoveCode(prefix, raw string, now time.Time) (serverMoveCode, error) {
	code := serverMoveCode{}
	encoded, ok := strings.CutPrefix(strings.TrimSpace(raw), prefix)
	if !ok {
		return code, errors.New("not a move code")
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || json.Unmarshal(data, &code) != nil {
		return code, errors.New("not a move code")
	}
	secret, err := hex.DecodeString(code.Secret)
	if serverMoveOrigin(code.To) == "" || !serverMoveIDPattern.MatchString(code.ID) || err != nil || len(secret) != 32 {
		return code, errors.New("not a move code")
	}
	if code.Also != "" && serverMoveOrigin(code.Also) == "" {
		code.Also = ""
	}
	if now.UnixMilli() > code.Expires {
		return code, errors.New("the move code ran out")
	}
	return code, nil
}

// serverMoveOrigin is an address as scheme and host alone, or "".
func serverMoveOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// serverMoveRequestOrigin is the address a request came to, which reached
// the new server from the person's computer: where the old server, often on
// that computer, sends.
func serverMoveRequestOrigin(request *http.Request) string {
	return serverMoveOrigin(requestScheme(request) + "://" + request.Host)
}

// countServerContents is what a move checks it carried: people, workspaces,
// pages, files and edits kept in page history. Its pages are the people's,
// in the trash too, and its edits theirs: not each workspace's People
// database and its people's pages, which the server makes and edits itself
// (workspaceArchiveTargetPageSQL) — the rule of every page count a person
// sees (2026-11-17: a server of five pages showed 7 sent and 7 taken in, and
// two edits of its People database among the edits).
func countServerContents(app core.App) administratorServerMoveCounts {
	counts := administratorServerMoveCounts{}
	_ = app.DB().NewQuery(`WITH [[server_made]] AS (
			SELECT [[page]].[[id]] FROM {{leafen_pages}} AS [[page]] WHERE NOT (`+workspaceArchiveTargetPageSQL("[[page]]")+`)
		)
		SELECT
		(SELECT COUNT(*) FROM {{users}}),
		(SELECT COUNT(*) FROM {{leafen_workspaces}}),
		(SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]] NOT IN (SELECT [[id]] FROM [[server_made]])),
		(SELECT COUNT(*) FROM {{leafen_files}}),
		(SELECT COUNT(*) FROM {{leafen_page_history_events}} WHERE [[page_id]] NOT IN (SELECT [[id]] FROM [[server_made]]))`).
		Row(&counts.People, &counts.Workspaces, &counts.Pages, &counts.Files, &counts.Edits)
	return counts
}

// sealForMove seals what goes beside the snapshot (the old server's replica
// mark) with the code's secret, as the snapshot is.
func sealForMove(secret string, plain []byte) (string, error) {
	gcm, err := serverMoveCipher(secret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, plain, nil)), nil
}

func openForMove(secret, sealed string) ([]byte, error) {
	gcm, err := serverMoveCipher(secret)
	if err != nil {
		return nil, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil || len(raw) < gcm.NonceSize() {
		return nil, errors.New("not encrypted for this move")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
}

func serverMoveCipher(secret string) (cipher.AEAD, error) {
	key, err := hex.DecodeString(secret)
	if err != nil || len(key) != 32 {
		return nil, errors.New("not a move secret")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// serverMovedIn is the new server's note of the move it took in.
type serverMovedIn struct {
	From     string                        `json:"from"`
	At       int64                         `json:"at"`
	Expected administratorServerMoveCounts `json:"expected"`
}

func readServerMovedIn(dataDir string) *serverMovedIn {
	note := &serverMovedIn{}
	if readRestoreFile(filepath.Join(dataDir, serverMovedInName), note) != nil {
		return nil
	}
	return note
}

// The moved fence is read on every live write (ygo_access_recheck.go), so it
// is looked at again only every second.
var dataMovedCache struct {
	mu    sync.Mutex
	dir   string
	at    time.Time
	moved bool
}

// dataMovedAway says this server's data went to another server, which
// answers for it: it takes no more changes, live ones included.
func dataMovedAway(app core.App) bool {
	if app == nil {
		return false
	}
	dataMovedCache.mu.Lock()
	defer dataMovedCache.mu.Unlock()
	dir := app.DataDir()
	if dataMovedCache.dir != dir || time.Since(dataMovedCache.at) > time.Second {
		_, err := os.Stat(filepath.Join(dir, replicaFencedName))
		dataMovedCache.dir, dataMovedCache.at, dataMovedCache.moved = dir, time.Now(), err == nil
	}
	return dataMovedCache.moved
}

func forgetDataMoved() {
	dataMovedCache.mu.Lock()
	dataMovedCache.at = time.Time{}
	dataMovedCache.mu.Unlock()
}

func registerServerMoveRoutes(event *core.ServeEvent) {
	in := serverMoveInOf(event.App)
	event.Router.GET(apiRoot+"/admin/server-move/in", in.read)
	event.Router.POST(apiRoot+"/admin/server-move/in", in.open)
	event.Router.DELETE(apiRoot+"/admin/server-move/in", in.close)
	event.Router.GET(apiRoot+"/server-move/in/{id}", in.info)
	event.Router.POST(apiRoot+"/server-move/in/{id}/finish", in.finish)
	// Each WebDAV method by name: one route for all would clash with the
	// app's own GET of every other path.
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions,
		"PROPFIND", "PROPPATCH", "MKCOL", "MOVE", "COPY", "LOCK", "UNLOCK"} {
		event.Router.Route(method, apiRoot+"/server-move/in/{id}/dav/{path...}", in.dav).Bind(apis.BodyLimit(0))
	}
	out := serverMoveOutOf(event.App)
	event.Router.GET(apiRoot+"/admin/server-move/out", out.read)
	event.Router.POST(apiRoot+"/admin/server-move/out", out.start)
}
