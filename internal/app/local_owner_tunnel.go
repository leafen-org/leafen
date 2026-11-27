package app

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Outside access to a packaged app's own server (decided 2026-10-13: the
// built-in tunnel's setting works there too; 2026-10-20: the server answers
// there as any server does, its devices, invitations and backup sign-ins
// included). A web agent's request to connect through the tunnel waits on a
// small page at the tunnel's address while the owner answers it in the app's
// window, and that page then carries the answer back to the agent.

const (
	localOwnerAgentRequestsPath = apiRoot + "/local-owner/agent-requests"
	localOwnerWaitingPath       = "/oauth/waiting"
	localOwnerWaitingStatusPath = "/oauth/waiting/status"

	localOwnerAgentWaiting = "waiting"
	localOwnerAgentAllowed = "allowed"
	localOwnerAgentDenied  = "denied"
	localOwnerAgentExpired = "expired"
)

// localOwnerWindowRoute says whether a path is the window's alone, which the
// outside address doesn't answer: the earlier owner's sign-in and the web
// agents waiting for the owner's answer.
func localOwnerWindowRoute(path string) bool {
	return path == localOwnerSessionPath || path == localOwnerSettledPath || path == localOwnerAgentRequestsPath
}

// localOwnerAgentRequest is a web agent's request to connect, or one from an
// agent on this computer, waiting for the owner's answer in the window.
type localOwnerAgentRequest struct {
	requestID string
	sealed    string
	ticket    string
	client    string
	here      bool
	created   int64
	expires   int64
	outcome   string
	redirect  string
}

type localOwnerAgentRequests struct {
	mu    sync.Mutex
	items map[string]*localOwnerAgentRequest
}

// sweep drops requests past their lifetime; the caller holds mu.
func (requests *localOwnerAgentRequests) sweep(now int64) {
	for id, item := range requests.items {
		if item.expires <= now {
			delete(requests.items, id)
		}
	}
}

// await keeps a request that came through the tunnel, or from this computer
// (here), and returns the ticket its waiting page asks with.
func (owner *LocalOwner) await(request mcpOAuthAuthorizationRequest, sealed, client string, here bool) (string, error) {
	secret := make([]byte, localOwnerCapabilityBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	ticket := base64.RawURLEncoding.EncodeToString(secret)
	now := time.Now().UnixMilli()
	owner.agents.mu.Lock()
	defer owner.agents.mu.Unlock()
	if owner.agents.items == nil {
		owner.agents.items = map[string]*localOwnerAgentRequest{}
	}
	owner.agents.sweep(now)
	owner.agents.items[request.ID] = &localOwnerAgentRequest{
		requestID: request.ID, sealed: sealed, ticket: ticket, client: client, here: here,
		created: now, expires: request.ExpiresAt, outcome: localOwnerAgentWaiting,
	}
	return ticket, nil
}

// answer keeps the owner's answer to a request that came through the tunnel,
// and reports whether it was one.
func (owner *LocalOwner) answer(requestID, outcome, redirect string) bool {
	owner.agents.mu.Lock()
	defer owner.agents.mu.Unlock()
	item, ok := owner.agents.items[requestID]
	if !ok || item.expires <= time.Now().UnixMilli() {
		return false
	}
	item.outcome, item.redirect = outcome, redirect
	return true
}

type localOwnerAgentRequestWire struct {
	Request string `json:"request"`
	Client  string `json:"client"`
	Created int64  `json:"created"`
	Here    bool   `json:"here,omitempty"`
}

// readAgentRequests lists, for the owner's window, the web agents
// waiting for an answer.
func (owner *LocalOwner) readAgentRequests(event *core.RequestEvent) error {
	if _, ok := authenticatedUser(event); !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	now := time.Now().UnixMilli()
	owner.agents.mu.Lock()
	owner.agents.sweep(now)
	waiting := []localOwnerAgentRequestWire{}
	for _, item := range owner.agents.items {
		if item.outcome == localOwnerAgentWaiting {
			waiting = append(waiting, localOwnerAgentRequestWire{Request: item.sealed, Client: item.client, Created: item.created, Here: item.here})
		}
	}
	owner.agents.mu.Unlock()
	sort.Slice(waiting, func(i, j int) bool { return waiting[i].Created < waiting[j].Created })
	return event.JSON(http.StatusOK, map[string]any{"requests": waiting})
}

// waitingStatus answers the waiting page: still waiting, or the address it
// goes on to.
func (owner *LocalOwner) waitingStatus(event *core.RequestEvent) error {
	ticket := event.Request.URL.Query().Get("ticket")
	now := time.Now().UnixMilli()
	owner.agents.mu.Lock()
	owner.agents.sweep(now)
	state, redirect := localOwnerAgentExpired, ""
	for _, item := range owner.agents.items {
		if ticket != "" && len(item.ticket) == len(ticket) && subtle.ConstantTimeCompare([]byte(item.ticket), []byte(ticket)) == 1 {
			state, redirect = item.outcome, item.redirect
			break
		}
	}
	owner.agents.mu.Unlock()
	event.Response.Header().Set("Cache-Control", "no-store")
	answer := map[string]string{"state": state}
	if redirect != "" {
		answer["redirectUrl"] = redirect
	}
	return event.JSON(http.StatusOK, answer)
}

// waitingPage is the page a web agent's browser waits on, in the person's
// language.
func (owner *LocalOwner) waitingPage(event *core.RequestEvent) error {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeLocalOwnerUnavailable, "Couldn’t open the page.", err)
	}
	script := base64.RawStdEncoding.EncodeToString(nonce)
	korean := strings.HasPrefix(strings.ToLower(event.Request.Header.Get("Accept-Language")), "ko")
	words := map[string]string{
		"lang": "en", "title": "Allow it in the Leafen app",
		"lead":    "A request to connect appeared in the Leafen app’s window on the computer. Allow it there, and this page goes back to the agent.",
		"waiting": "Waiting for the answer…", "denied": "The request was declined in the Leafen app.",
		"expired": "This request ran out. Connect again from the agent.",
	}
	if korean {
		words = map[string]string{
			"lang": "ko", "title": "리펜 앱에서 허용하세요",
			"lead":    "이 컴퓨터의 리펜 앱 창에 연결 요청이 표시되었습니다. 거기서 허용하면 이 페이지가 에이전트로 돌아갑니다.",
			"waiting": "답을 기다리는 중…", "denied": "리펜 앱에서 요청을 거부했습니다.",
			"expired": "요청 시간이 지났습니다. 에이전트에서 다시 연결하세요.",
		}
	}
	status := localOwnerWaitingStatusPath + "?ticket=" + url.QueryEscape(event.Request.URL.Query().Get("ticket"))
	header := event.Response.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+script+"'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	page := `<!doctype html><html lang="` + words["lang"] + `"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(words["title"]) + `</title>
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#37352f;background:#fff}
main{max-width:420px;padding:24px}h1{font-size:22px;margin:0 0 8px}p{margin:0 0 12px;color:#5f5e5b}#state{color:#787774}#state.error{color:#d44c47}
@media (prefers-color-scheme:dark){body{background:#191919;color:#e3e2e0}p{color:#aeada9}}</style></head>
<body><main><h1>` + html.EscapeString(words["title"]) + `</h1><p>` + html.EscapeString(words["lead"]) + `</p><p id="state" role="status">` + html.EscapeString(words["waiting"]) + `</p></main>
<script nonce="` + script + `">
const state = document.getElementById("state");
const words = { denied: ` + jsString(words["denied"]) + `, expired: ` + jsString(words["expired"]) + ` };
async function ask() {
  try {
    const answer = await (await fetch(` + jsString(status) + `, { cache: "no-store" })).json();
    if (answer.redirectUrl) { location.replace(answer.redirectUrl); return; }
    if (answer.state === "denied" || answer.state === "expired") { state.textContent = words[answer.state]; state.className = "error"; return; }
  } catch {}
  setTimeout(ask, 2000);
}
ask();
</script></body></html>`
	return event.HTML(http.StatusOK, page)
}

// jsString is text as a JavaScript string literal safe inside a script tag.
func jsString(text string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "<", `<`, ">", `>`, "&", `&`, "\n", `\n`, "\r", `\r`, " ", ` `, " ", ` `)
	return `"` + replacer.Replace(text) + `"`
}
