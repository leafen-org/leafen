package app

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

const (
	localOwnerStoreKey = "leafen.localOwner"
	// The account address an earlier version gave the hidden owner it made
	// for the app (owner@local), one no mail reaches; that owner becomes the
	// account it always was, with no email, once its window keeps a sign-in.
	localOwnerEmail                       = "owner@local.leafen.invalid"
	localOwnerSessionPath                 = apiRoot + "/local-owner/session"
	localOwnerSettledPath                 = apiRoot + "/local-owner/settled"
	localOwnerCapabilityBytes             = 32
	errorCodeLocalOwnerAddressRejected    = "LEAFEN_LOCAL_OWNER_ADDRESS_REJECTED"
	errorCodeLocalOwnerCapabilityRejected = "LEAFEN_LOCAL_OWNER_CAPABILITY_REJECTED"
	errorCodeLocalOwnerUnavailable        = "LEAFEN_LOCAL_OWNER_UNAVAILABLE"
	// Ahead of PocketBase's own CORS, which would answer any page.
	localOwnerGuardPriority = apis.DefaultCorsMiddlewarePriority - 10
)

// LocalOwner is the server a packaged client runs on its device, as decided
// 2026-09-24 and changed 2026-10-20 (the multi-server decision: one account
// flow for every server): it answers only its own window's address and
// takes no sign-ups, but its accounts are made and signed in to as on any
// server. Its first setup, a name, is opened by the setup code the shell
// hands its window alone. An earlier version made a hidden owner that the
// window signed in with a capability; that owner's window signs in with it
// once more, keeps the sign-in, and the capability ends.
type LocalOwner struct {
	origin     string
	host       string
	capability string
	// Web agents' requests to connect through the tunnel, waiting for the
	// owner's answer (local_owner_tunnel.go).
	agents localOwnerAgentRequests
	// How the shell opens one of the server's screens in its window (ShowsScreens).
	screens atomic.Pointer[func(path string)]
}

type localOwnerSessionInput struct {
	Code string `json:"code"`
}

// EnableLocalOwner makes pb a packaged client's own server, answering only
// at origin ("http://127.0.0.1:<port>").
func EnableLocalOwner(pb *pocketbase.PocketBase, origin string) (*LocalOwner, error) {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.Path != "" || parsed.RawQuery != "" || parsed.User != nil {
		return nil, fmt.Errorf("local owner origin %q is not an http origin", origin)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil || port == "" || !net.ParseIP(host).IsLoopback() {
		return nil, fmt.Errorf("local owner origin %q is not a loopback address", origin)
	}
	secret := make([]byte, localOwnerCapabilityBytes)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	owner := &LocalOwner{
		origin:     parsed.Scheme + "://" + parsed.Host,
		host:       parsed.Host,
		capability: base64.RawURLEncoding.EncodeToString(secret),
	}
	pb.Store().Set(localOwnerStoreKey, owner)
	pb.OnServe().BindFunc(func(event *core.ServeEvent) error {
		owner.register(event)
		return event.Next()
	})
	return owner, nil
}

// register guards the server's routes and adds the earlier owner's sign-in.
// The setup page isn't opened in a browser: the app's window shows it.
func (owner *LocalOwner) register(event *core.ServeEvent) {
	event.InstallerFunc = nil
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "leafenLocalOwnerGuard", Priority: localOwnerGuardPriority, Func: owner.guard,
	})
	event.Router.POST(localOwnerSessionPath, owner.exchange)
	event.Router.POST(localOwnerSettledPath, owner.settle)
	event.Router.GET(localOwnerAgentRequestsPath, owner.readAgentRequests)
	event.Router.GET(localOwnerWaitingPath, owner.waitingPage)
	event.Router.GET(localOwnerWaitingStatusPath, owner.waitingStatus)
}

// Capability is what the shell hands its window to sign an earlier version's
// owner in, until the window keeps a sign-in of its own (Waiting).
func (owner *LocalOwner) Capability() string {
	return owner.capability
}

// Origin is the only page address the server answers.
func (owner *LocalOwner) Origin() string {
	return owner.origin
}

// SetupCode is the code that opens the server's first setup, which the shell
// hands its window alone, or "" once the server is set up.
func (owner *LocalOwner) SetupCode(app core.App) string {
	if configured, err := administratorConfigured(app); err != nil || configured {
		return ""
	}
	return setupCodeStateOf(app).link
}

// Waiting says whether an earlier version's hidden owner is still waiting
// for its window to keep a sign-in.
func (owner *LocalOwner) Waiting(app core.App) bool {
	_, err := app.FindAuthRecordByEmail("users", localOwnerEmail)
	return err == nil
}

func localOwnerOf(app core.App) *LocalOwner {
	owner, _ := app.Store().Get(localOwnerStoreKey).(*LocalOwner)
	return owner
}

// guard turns away what isn't the owner's window: a request naming another
// host, as a rebound name does, or sent from a page elsewhere than the
// server's own or the packaged client's. At the outside address the owner
// turned on — the tunnel's, or an earlier one forwarding here — the server
// answers as any server does (the multi-server decision, 2026-10-20: devices,
// invitations and backup sign-ins come in there too), but for the window's
// own routes. The window's frame of a provider's player comes in at
// localhost and passes (localhostProviderFrame). What the server sends its
// own router — a bot's leafen_call, an app's data, the Notion bundle — came
// in here first and passes.
func (owner *LocalOwner) guard(event *core.RequestEvent) error {
	if isServerInnerRequest(event.Request) {
		return event.Next()
	}
	if event.Request.Host != owner.host {
		if owner.localhostProviderFrame(event.Request) {
			return event.Next()
		}
		outside := requestTunnelOrigin(event.Request) != "" || EarlierAddressOf(event.Request) != ""
		if outside && !localOwnerWindowRoute(event.Request.URL.Path) {
			return event.Next()
		}
		return apiError(event, http.StatusMisdirectedRequest, errorCodeLocalOwnerAddressRejected, "This server only answers its own app.")
	}
	if origin := event.Request.Header.Get("Origin"); origin != "" && origin != owner.origin && !isPackagedClientOrigin(origin) &&
		!localOwnerAppFrameRequest(origin, event.Request.URL.Path) {
		return apiError(event, http.StatusForbidden, errorCodeLocalOwnerAddressRejected, "This server only answers its own app.")
	}
	return event.Next()
}

// localhostProviderFrame is the window's frame of a streaming provider's
// player (provider_frame.go), which the page opens at localhost on the
// server's own port (web mediaEmbeds.ts providerFrameSrc): YouTube refuses
// some videos to a page named by a number. localhost is this computer's own
// name, which no rebound name can be, and the page reads nothing of the
// server; turned away there, every YouTube, Vimeo and Loom embed in the
// app's own workspace showed this guard's error in place of its player
// (2026-11-17).
func (owner *LocalOwner) localhostProviderFrame(request *http.Request) bool {
	_, port, err := net.SplitHostPort(owner.host)
	return err == nil && (request.Method == http.MethodGet || request.Method == http.MethodHead) &&
		request.URL.Path == apiRoot+providerFramePath && strings.EqualFold(request.Host, "localhost:"+port)
}

// localOwnerAppFrameRequest is an app's frame asking for what its own
// signatures open: its files under the frame's signed ticket, and its rooms
// with their own tokens. A sandboxed frame has no origin, so it says "null";
// turned away, an app's module scripts never ran and its screen stayed blank
// in the desktop app (2026-11-05).
func localOwnerAppFrameRequest(origin, path string) bool {
	return origin == "null" && originlessFrameRoute(path)
}

func (owner *LocalOwner) accepts(capability string) bool {
	return len(capability) == len(owner.capability) &&
		subtle.ConstantTimeCompare([]byte(capability), []byte(owner.capability)) == 1
}

// exchange signs an earlier version's owner in on the window holding the
// capability, as a registered device (signed in until signed out),
// replacing their oldest sign-in when they have as many as they may.
func (owner *LocalOwner) exchange(event *core.RequestEvent) error {
	input := localOwnerSessionInput{}
	if err := event.BindBody(&input); err != nil || !owner.accepts(input.Code) {
		return apiError(event, http.StatusUnauthorized, errorCodeLocalOwnerCapabilityRejected, "Open Leafen from its app to log in.")
	}
	user, err := event.App.FindAuthRecordByEmail("users", localOwnerEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusConflict, errorCodeLocalOwnerUnavailable, "This server has no owner to log in.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeLocalOwnerUnavailable, "Couldn't log the owner in.", err)
	}
	session, err := issueAuthenticatedBrowserSession(
		event.App, user, browserSessionMetadataFromEvent(event), true, authSessionMethodDevice, "",
	)
	if err != nil {
		return browserSessionAPIError(event, err)
	}
	return event.JSON(http.StatusCreated, session)
}

// settle ends the earlier owner's capability sign-in once its window says
// it keeps a sign-in of its own: the account loses the address no mail
// reaches and is an account like any other, with no email.
func (owner *LocalOwner) settle(event *core.RequestEvent) error {
	user, ok, err := signedInPerson(event)
	if !ok {
		return err
	}
	input := localOwnerSessionInput{}
	if err := event.BindBody(&input); err != nil || !owner.accepts(input.Code) {
		return apiError(event, http.StatusUnauthorized, errorCodeLocalOwnerCapabilityRejected, "Open Leafen from its app to log in.")
	}
	if !strings.EqualFold(user.Email(), localOwnerEmail) {
		return event.NoContent(http.StatusNoContent)
	}
	if err := endEarlierOwnerAddress(event.App, user.Id); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeLocalOwnerUnavailable, "Couldn't finish moving the owner's login.", err)
	}
	return event.NoContent(http.StatusNoContent)
}

// settleEarlierOwnerElsewhere does the same at once for data an earlier app
// made, moved or restored onto a server that isn't an app's own: no window
// there holds the capability, and the owner gets in with a device code
// (`leafen account device-code --name`). A server whose data moved on
// leaves it as it was.
func settleEarlierOwnerElsewhere(app core.App) {
	if localOwnerOf(app) != nil || readReplicaFence(app.DataDir()) != nil {
		return
	}
	user, err := app.FindAuthRecordByEmail("users", localOwnerEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return
	}
	if err == nil {
		err = endEarlierOwnerAddress(app, user.Id)
	}
	if err != nil {
		app.Logger().Warn("The account an earlier app made for its owner keeps the address no mail reaches", "error", err)
	}
}

func endEarlierOwnerAddress(app core.App, userID string) error {
	return app.RunInTransaction(func(txApp core.App) error {
		if _, err := txApp.DB().NewQuery(`UPDATE {{users}} SET [[email]] = '' WHERE [[id]] = {:id} AND [[email]] = {:email}`).
			Bind(dbx.Params{"id": userID, "email": localOwnerEmail}).Execute(); err != nil {
			return err
		}
		// The earlier app's server took no sign-ups, whatever its settings
		// said; it is closed as a server set up now is, open to the invited.
		if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_server_settings}}
			SET [[signup_policy]] = {:closed}, [[revision]] = [[revision]] + 1, [[updated_at]] = {:now}
			WHERE [[id]] = 'default' AND [[signup_policy]] <> {:closed}`).
			Bind(dbx.Params{"closed": administratorSignupPolicyClosed, "now": time.Now().UnixMilli()}).Execute(); err != nil {
			return err
		}
		// The owner's password was the app's, which no one knows.
		return markAccountPasswordUnknown(txApp, userID)
	})
}
