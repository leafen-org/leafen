package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A browser's sign-in carries over to the server's new address (2026-10-20
// "주소가 여러 번 바뀌었다 해도 이어받기"): a browser keeps its sign-in by
// address, so a page opened at an earlier address that still forwards gets a
// small page there instead of the redirect. That page, with the sign-in the
// browser kept there, asks for a pass — used once, for two minutes, tied to
// that sign-in — and opens the same place at the new address with the pass
// in its fragment, which no server or log sees; the new address trades it
// for this device's sign-in there, and the one at the earlier address ends.
// The page also hands over the push subscription its address held, which
// ends with it, so the new address asks to turn notifications on again. A
// page with no sign-in kept, or one the server refuses, just goes on to the
// new address. Requests other than page loads are redirected as before.

const (
	addressHandoffIssuePath    = "/account/address-handoff"
	addressHandoffExchangePath = "/auth/address-handoff/exchange"
	addressHandoffLifetime     = 2 * time.Minute
	addressHandoffPassBytes    = 32
	addressHandoffFragment     = "leafen-handoff"
)

var errAddressHandoffInvalid = errors.New(errorCodeDeviceCodeInvalid)

func registerAddressHandoffRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+addressHandoffIssuePath, issueAddressHandoff)
	event.Router.POST(apiRoot+addressHandoffExchangePath, exchangeAddressHandoff)
}

// issueAddressHandoff gives the signed-in browser at an earlier address a
// pass to its sign-in at the new one.
func issueAddressHandoff(event *core.RequestEvent) error {
	user, ok, err := signedInPerson(event)
	if !ok {
		return err
	}
	sessionID := currentBrowserSessionID(event)
	if sessionID == "" {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := authAddressHandoffInput{}
	_ = event.BindBody(&input)
	from := EarlierAddressOf(event.Request)
	to := forwardedOrigin(from)
	if from == "" || to == "" {
		return apiError(event, http.StatusConflict, errorCodeDeviceCodeInvalid, "This address doesn't forward to another.")
	}
	random := make([]byte, addressHandoffPassBytes)
	if _, err := rand.Read(random); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeDeviceCodeFailed, "Couldn't make the pass.", err)
	}
	pass := base64.RawURLEncoding.EncodeToString(random)
	now := time.Now().UTC().UnixMilli()
	err = event.App.RunInTransaction(func(txApp core.App) error {
		notifications := 0
		// The push subscription the earlier address held ends here; the new
		// address asks to turn notifications on again.
		if endpoint := strings.TrimSpace(input.PushEndpoint); endpoint != "" && len(endpoint) <= 2048 {
			result, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_web_push_subscriptions}}
				WHERE [[user_id]] = {:userId} AND [[endpoint]] = {:endpoint}`).
				Bind(dbx.Params{"userId": user.Id, "endpoint": endpoint}).Execute()
			if err != nil {
				return err
			}
			if removed, _ := result.RowsAffected(); removed > 0 {
				notifications = 1
			}
		}
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_auth_address_handoffs}}
			WHERE [[owner_id]] = {:ownerId} AND [[expires_at]] <= {:now}`).
			Bind(dbx.Params{"ownerId": user.Id, "now": now}).Execute(); err != nil {
			return err
		}
		_, err := txApp.DB().NewQuery(`INSERT INTO {{leafen_auth_address_handoffs}}
			([[pass_hash]], [[session_id]], [[owner_id]], [[from_origin]], [[notifications]], [[expires_at]], [[created_at]])
			VALUES ({:hash}, {:sessionId}, {:ownerId}, {:from}, {:notifications}, {:expires}, {:now})`).
			Bind(dbx.Params{"hash": mcpTokenHash(pass), "sessionId": sessionID, "ownerId": user.Id, "from": from,
				"notifications": notifications, "expires": now + addressHandoffLifetime.Milliseconds(), "now": now}).Execute()
		return err
	})
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeDeviceCodeFailed, "Couldn't make the pass.", err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusCreated, authAddressHandoffResponse{Pass: pass, To: to})
}

// exchangeAddressHandoff signs the browser in at the new address with its
// pass, once, ending its sign-in at the earlier one; it says whether that
// address held its notifications and whether its passkeys were made there.
func exchangeAddressHandoff(event *core.RequestEvent) error {
	input := authAddressHandoffExchangeInput{}
	_ = event.BindBody(&input)
	if strings.TrimSpace(input.Pass) == "" {
		return refuseMissingInput(event, errorCodeDeviceCodeInvalid, "pass", "the pass the address it moved from gave.")
	}
	pass := strings.TrimSpace(input.Pass)
	metadata := browserSessionMetadataFromEvent(event)
	response := authAddressHandoffExchangeResponse{}
	from := ""
	ownerID := ""
	err := errAddressHandoffInvalid
	if pass != "" && len(pass) <= 128 {
		err = event.App.RunInTransaction(func(txApp core.App) error {
			now := time.Now().UTC().UnixMilli()
			stored := struct {
				SessionID     string `db:"session_id"`
				OwnerID       string `db:"owner_id"`
				FromOrigin    string `db:"from_origin"`
				Notifications bool   `db:"notifications"`
			}{}
			err := txApp.DB().NewQuery(`SELECT [[session_id]], [[owner_id]], [[from_origin]], [[notifications]]
				FROM {{leafen_auth_address_handoffs}} WHERE [[pass_hash]] = {:hash} AND [[used_at]] = 0 AND [[expires_at]] > {:now}`).
				Bind(dbx.Params{"hash": mcpTokenHash(pass), "now": now}).One(&stored)
			if errors.Is(err, sql.ErrNoRows) {
				return errAddressHandoffInvalid
			}
			if err != nil {
				return err
			}
			// The sign-in at the earlier address must still be open.
			previous := struct {
				SecondFactor string `db:"second_factor"`
			}{}
			err = txApp.DB().NewQuery(`SELECT [[second_factor]] FROM {{leafen_auth_sessions}}
				WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0 AND [[expires_at]] > {:now}`).
				Bind(dbx.Params{"id": stored.SessionID, "ownerId": stored.OwnerID, "now": now}).One(&previous)
			if errors.Is(err, sql.ErrNoRows) {
				return errAddressHandoffInvalid
			}
			if err != nil {
				return err
			}
			user, err := txApp.FindRecordById("users", stored.OwnerID)
			if err != nil {
				return refusedUnlessStorage(errAddressHandoffInvalid, err)
			}
			ownerID, from = user.Id, stored.FromOrigin
			if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_auth_address_handoffs}} SET [[used_at]] = {:now}
				WHERE [[pass_hash]] = {:hash} AND [[used_at]] = 0`).Bind(dbx.Params{"hash": mcpTokenHash(pass), "now": now}).Execute(); err != nil {
				return err
			}
			// The earlier address's sign-in ends first, so the device takes its place
			// rather than another device's.
			if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_auth_sessions}} SET [[revoked_at]] = {:now}, [[updated_at]] = {:now}
				WHERE [[id]] = {:id} AND [[revoked_at]] = 0`).Bind(dbx.Params{"id": stored.SessionID, "now": now}).Execute(); err != nil {
				return err
			}
			session, err := issueAuthenticatedBrowserSessionInTransaction(
				txApp, user, metadata, false, authSessionMethodDevice, "", previous.SecondFactor,
			)
			if err != nil {
				return err
			}
			response.Session, response.Notifications = session, stored.Notifications
			return recordAuthAttemptInTransactionSuccessWithMetadata(
				txApp, user.Id, user.Email(), authAuditMethodDeviceCode, authAuditPhaseVerify, metadata,
			)
		})
	}
	if err != nil {
		if errors.Is(err, errAddressHandoffInvalid) {
			return apiError(event, http.StatusBadRequest, errorCodeDeviceCodeInvalid, "That pass isn't one this server gave, or it was used or ran out.")
		}
		return deviceCodeAPIError(event, err)
	}
	// Passkeys are an address's: ones made at the earlier address only ask
	// for one here.
	response.Passkey = passkeysOnlyAt(event.App, ownerID, hostOf(from), event.Request.Host)
	recheckCollaborationAccessAfterCommit(event.App)
	endSignInWaits(event.App, ownerID)
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusCreated, response)
}

// passkeysOnlyAt says whether a person's passkeys include one for the
// earlier host and none for the current one.
func passkeysOnlyAt(app core.App, userID, earlier, current string) bool {
	if name, _, found := strings.Cut(current, ":"); found {
		current = name
	}
	if userID == "" || earlier == "" {
		return false
	}
	hosts := []string{}
	if err := app.DB().NewQuery(`SELECT DISTINCT [[rp_id]] FROM {{leafen_passkeys}} WHERE [[user_id]] = {:userId}`).
		Bind(dbx.Params{"userId": userID}).Column(&hosts); err != nil {
		return false
	}
	at, here := false, false
	for _, host := range hosts {
		at = at || strings.EqualFold(host, earlier)
		here = here || strings.EqualFold(host, current)
	}
	return at && !here
}

// addressHandoffNavigation says whether a request at an earlier address is
// a page being opened, which the handoff page answers.
func addressHandoffNavigation(request *http.Request) bool {
	if request.Method != http.MethodGet {
		return false
	}
	path := request.URL.Path
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/mcp") || strings.HasPrefix(path, "/.well-known/") ||
		strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/_/") {
		return false
	}
	if mode := request.Header.Get("Sec-Fetch-Mode"); mode != "" {
		// A page's service worker asks for it as a navigation with no
		// destination, so a browser that has been here gets the handoff too.
		dest := request.Header.Get("Sec-Fetch-Dest")
		return mode == "navigate" && (dest == "document" || dest == "empty")
	}
	return strings.Contains(request.Header.Get("Accept"), "text/html")
}

// serveAddressHandoffPage answers a page opened at an earlier address with
// the page that carries its sign-in over to the new address.
func serveAddressHandoffPage(writer http.ResponseWriter, request *http.Request, to string) {
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	script := base64.StdEncoding.EncodeToString(nonce)
	target, _ := json.Marshal(strings.TrimRight(to, "/") + request.URL.RequestURI())
	header := writer.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+script+"'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><meta name="referrer" content="no-referrer">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light dark"><title>Leafen</title></head>` +
		`<body style="font:15px system-ui,sans-serif;color:#787774;display:grid;place-items:center;height:100vh;margin:0">` +
		`<p>` + html.EscapeString(addressHandoffWords(request, hostOf(to))) + `</p>` +
		`<script nonce="` + script + `">(async()=>{const to=` + string(target) + `;let go=to+location.hash;` +
		`try{const kept=localStorage.getItem("leafen:v2:session");const token=kept&&JSON.parse(kept).token;` +
		`if(token){let pushEndpoint="";try{const worker=await navigator.serviceWorker?.getRegistration();` +
		`const subscription=await worker?.pushManager?.getSubscription();if(subscription){pushEndpoint=subscription.endpoint;await subscription.unsubscribe();}}catch{}` +
		`const answer=await fetch("` + apiRoot + addressHandoffIssuePath + `",{method:"POST",headers:{authorization:"Bearer "+token,"content-type":"application/json"},body:JSON.stringify({pushEndpoint})});` +
		`if(answer.ok){const {pass}=await answer.json();localStorage.removeItem("leafen:v2:session");` +
		`const rest=location.hash.slice(1);go=to+"#` + addressHandoffFragment + `="+encodeURIComponent(pass)+(rest?"&then="+encodeURIComponent(rest):"");}}}catch{}` +
		`location.replace(go);})();</script></body></html>`))
}

// addressHandoffWords says where the server went, in the browser's language
// where the page has it: nothing here reads the app's catalogs, and the moment
// is short.
func addressHandoffWords(request *http.Request, host string) string {
	if strings.HasPrefix(strings.ToLower(request.Header.Get("Accept-Language")), "ko") {
		return "리펜 주소가 " + host + "(으)로 바뀌었습니다…"
	}
	return "Leafen moved to " + host + "…"
}

// handoffTarget is the new address of a forward, for the page.
func handoffTarget(forward addressForward) string {
	parsed, err := url.Parse(forward.To)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
