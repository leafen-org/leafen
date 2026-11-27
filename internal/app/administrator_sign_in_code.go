package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A first administrator made with a name alone has no password, so landing
// signed out — whatever the cause — must not keep them out of their own
// server (user decision 2026-11-19 "넣어", the third structural fix for the
// first setup's sign-out). A server install — Docker, a NAS, the program
// alone — shows a one-time device code for its administrator where only its
// operator reads: its own console log, as the setup link is, and `leafen
// setup status`. It shows one only while none of its administrators has a
// page of it open (no live socket: a window that lost its sign-in has none)
// and only when asked — the sign-in page's ask from the server's own
// network, which says where the code is, or the status command — one at a
// time: ten minutes, once, never in an address, and joined at "Sign in with
// a code" as any device code is, whose wrong tries say nothing of whose code
// it was and are limited. The asks are limited by address and across the
// server as those tries are, and the server audit log keeps each code made.
// The apps' own servers show none: their window signs its owner back in
// (desktop/device_owner.go).

const (
	administratorSignInCodeStoreKey = "leafen.administratorSignInCode"
	administratorSignInCodePath     = apiRoot + "/auth/administrator-code"
	accountAuditDeviceCodeServerLog = "device-code-server-log"
)

type administratorSignInCode struct {
	mu sync.Mutex
	// The code the log showed last: its row, its account and how it reads.
	id, ownerID, code string
	expiresAt         int64
	asks              *deviceCodeLimiter
}

func administratorSignInCodeOf(app core.App) *administratorSignInCode {
	return app.Store().GetOrSet(administratorSignInCodeStoreKey, func() any {
		return &administratorSignInCode{asks: &deviceCodeLimiter{
			byAddress: newRequestWindowLimiter(deviceCodeFailuresByAddress, deviceCodeFailureSpan),
			server:    newRequestWindowLimiter(deviceCodeFailuresOnServer, deviceCodeServerSpan),
		}}
	}).(*administratorSignInCode)
}

// administratorAway is the administrator a code would sign in — the one who
// set the server up while still an active administrator, else the longest
// one — when the server is set up, isn't an app's own, and none of its active
// administrators has a page of it open now; "" otherwise.
func administratorAway(app core.App) (string, error) {
	if localOwnerOf(app) != nil {
		return "", nil
	}
	administrators := []string{}
	if err := app.DB().NewQuery(`SELECT [[administrator]].[[user_id]] FROM {{leafen_administrators}} [[administrator]]
		LEFT JOIN {{leafen_server_settings}} [[settings]] ON [[settings]].[[id]] = 'default'
		LEFT JOIN {{leafen_administrator_account_states}} [[state]] ON [[state]].[[user_id]] = [[administrator]].[[user_id]]
		WHERE COALESCE([[state]].[[status]], 'active') = 'active'
		ORDER BY [[administrator]].[[user_id]] = COALESCE([[settings]].[[setup_admin_id]], '') DESC,
			[[administrator]].[[created_at]], [[administrator]].[[user_id]]`).Column(&administrators); err != nil {
		return "", err
	}
	if len(administrators) == 0 {
		return "", nil
	}
	if rooms, _ := app.Store().Get(ygoRoomAuthorizationStoreKey).(*ygoRoomAuthorization); rooms != nil {
		for _, administrator := range administrators {
			if rooms.personOnline(administrator) {
				return "", nil
			}
		}
	}
	return administrators[0], nil
}

// personOnline says whether the person has a page or a workspace of the
// server open now: a live socket counted against them.
func (a *ygoRoomAuthorization) personOnline(userID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.personSockets[userID] > 0
}

// current answers the code the log shows now while its administrator is
// still away, or "" with none; asked to show one, it makes a new code and
// prints it where none is live. A code is live until used or out of time.
func (shown *administratorSignInCode) current(app core.App, show bool) (string, int64, error) {
	ownerID, err := administratorAway(app)
	if err != nil || ownerID == "" {
		return "", 0, err
	}
	shown.mu.Lock()
	defer shown.mu.Unlock()
	if shown.ownerID == ownerID && shown.id != "" {
		live := 0
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_account_device_codes}}
			WHERE [[id]] = {:id} AND [[used_at]] = 0 AND [[expires_at]] > {:now}`).
			Bind(dbx.Params{"id": shown.id, "now": time.Now().UTC().UnixMilli()}).Row(&live); err != nil {
			return "", 0, err
		}
		if live == 1 {
			return shown.code, shown.expiresAt, nil
		}
	}
	shown.id, shown.ownerID, shown.code, shown.expiresAt = "", "", "", 0
	if !show {
		return "", 0, nil
	}
	user, err := app.FindRecordById("users", ownerID)
	if err != nil {
		return "", 0, err
	}
	issued, err := issueServerOwnDeviceCode(app, ownerID, accountAuditDeviceCodeServerLog)
	if errors.Is(err, errSAMLRequired) || errors.Is(err, errDeviceSignInRestricted) || errors.Is(err, errDeviceOwnerAccountClosed) ||
		errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	shown.id, shown.ownerID, shown.code, shown.expiresAt = issued.ID, ownerID, issued.Code, issued.ExpiresAt
	// Standard output, as the setup link: the console and the container's log, not the server's stored log.
	fmt.Printf("\nNo administrator of this Leafen has it open on a device now. A one-time code logs one device in to %s, "+
		"for ten minutes, once: %s\nOn the login page, choose \"Log in with a code\" and type it. `leafen setup status` shows it too.\n",
		accountLabel(user), issued.Code)
	return issued.Code, issued.ExpiresAt, nil
}

func registerAdministratorSignInCodeRoute(event *core.ServeEvent) {
	event.Router.POST(administratorSignInCodePath, askAdministratorSignInCode)
}

// askAdministratorSignInCode is the sign-in page asking the server to show
// its administrator a code in its log, while none has a page of it open; it
// answers whether one is shown there, never the code. Only a page opened on
// the server's own network asks it so (requestFromNetwork, the first setup's
// rule; user 2026-11-23): one from outside — a guest shown a shared page —
// makes no code and is answered none shown, so its sign-in page says nothing
// of an administrator's way back.
func askAdministratorSignInCode(event *core.RequestEvent) error {
	event.Response.Header().Set("Cache-Control", "no-store")
	if !requestFromNetwork(event.Request) {
		return event.JSON(http.StatusOK, map[string]bool{"shown": false})
	}
	state := administratorSignInCodeOf(event.App)
	address, now := "address:"+requestLimitAddress(event.RealIP()), time.Now()
	if admitted, _ := state.asks.byAddress.admit(address, now); !admitted {
		return deviceCodeAPIError(event, errDeviceCodeLimited)
	}
	if admitted, _ := state.asks.server.admit("server", now); !admitted {
		state.asks.byAddress.refund(address)
		return deviceCodeAPIError(event, errDeviceCodeLimited)
	}
	code, _, err := state.current(event.App, true)
	if err != nil {
		event.App.Logger().Warn("leafen administrator sign-in code not shown", "error", err)
	}
	return event.JSON(http.StatusOK, map[string]bool{"shown": code != ""})
}
