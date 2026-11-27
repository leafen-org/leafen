package app

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// The first setup from the outside internet takes a one-time code (the
// multi-server decision's 2026-10-20 change: no one becomes the
// administrator just by opening a new server first, as bots scan for them):
// the code in the setup link the server prints, which lasts while it runs
// unset, or the one the operator gave it in LEAFEN_SETUP_CODE (a hosting
// template makes one). From this computer or its network it takes none
// (2026-11-19, setup_same_network.go). An operator running closed, automated
// servers turns the code off with LEAFEN_SETUP_WITHOUT_CODE=1. Wrong codes
// are limited by address.

const (
	setupCodeEnv        = "LEAFEN_SETUP_CODE"
	setupWithoutCodeEnv = "LEAFEN_SETUP_WITHOUT_CODE"
	setupCodeStoreKey   = "leafen.setupCode"
	// Of the device code's digits: 36 are more than the 24 of thirty letters
	// and digits before (10^36 > 30^24).
	setupLinkCodeLength        = 36
	setupCodeLeastLength       = 8
	setupCodeFailuresByAddress = 10
	setupCodeFailureSpan       = 15 * time.Minute
)

type setupCodeState struct {
	link     string
	failures *requestWindowLimiter
}

func setupCodeStateOf(app core.App) *setupCodeState {
	return app.Store().GetOrSet(setupCodeStoreKey, func() any {
		return &setupCodeState{
			link:     security.RandomStringWithAlphabet(setupLinkCodeLength, deviceCodeAlphabet),
			failures: newRequestWindowLimiter(setupCodeFailuresByAddress, setupCodeFailureSpan),
		}
	}).(*setupCodeState)
}

// setupCodeRequired says whether the code is on at all: an operator may turn
// it off (setupCodeAsked says whom it is asked of).
func setupCodeRequired() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(setupWithoutCodeEnv))) {
	case "1", "true", "on", "yes":
		return false
	}
	return true
}

// setupCodeAsked says whether the first setup asks this request for a code:
// one from outside the server's network, where the code is on; and always on
// a packaged client's own server, whose window alone sets it up with the
// code its shell hands it.
func setupCodeAsked(event *core.RequestEvent) bool {
	if !setupCodeRequired() {
		return false
	}
	return localOwnerOf(event.App) != nil || !requestFromNetwork(event.Request)
}

// operatorSetupCode is the code the operator gave, when long enough to keep.
func operatorSetupCode() string {
	code := strings.TrimSpace(os.Getenv(setupCodeEnv))
	if len(code) < setupCodeLeastLength {
		return ""
	}
	return code
}

// setupCodeAccepts says whether a code typed or carried in the setup link
// opens the first setup. The link's code, of the device code's digits, is
// read as a device code is (readTypedCode); the operator's own, of any
// letters, as given or as typed with a Korean keyboard on (typed_code_keys.go).
func setupCodeAccepts(app core.App, code string) bool {
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}
	matches := func(accepted string, typed ...string) bool {
		for _, candidate := range typed {
			if accepted != "" && len(accepted) == len(candidate) && subtle.ConstantTimeCompare([]byte(accepted), []byte(candidate)) == 1 {
				return true
			}
		}
		return false
	}
	return matches(setupCodeStateOf(app).link, code, readTypedCode(code)) ||
		matches(operatorSetupCode(), code, typedCodeKeys(code))
}

// setupLinkFragment is what the printed setup link ends with: the code, in
// the fragment so that it stays out of logs and referrers.
func setupLinkFragment(app core.App) string {
	if !setupCodeRequired() {
		return ""
	}
	return "#code=" + setupCodeStateOf(app).link
}

// admitFirstSetup lets a first setup through — making the first
// administrator or restoring a backup — while the server isn't set up, with
// the setup code where one is asked. A server set up already says so before
// any code is read, so a request after the setup is no wrong try.
func admitFirstSetup(event *core.RequestEvent, setupCode string) bool {
	configured, err := administratorConfiguredContext(event.Request.Context(), event.App)
	if err != nil {
		_ = apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the setup state.", err)
		return false
	}
	if configured {
		_ = apiError(event, http.StatusConflict, errorCodeSetupClosed, "Server setup is already complete.")
		return false
	}
	if !setupCodeAsked(event) {
		return true
	}
	// No code at all is said as missing, not wrong, and is no wrong try
	// (2026-11-25: a restore from outside without one read "incorrect").
	if strings.TrimSpace(setupCode) == "" {
		_ = apiError(event, http.StatusForbidden, errorCodeSetupCodeRequired,
			"Enter the setup code: open the setup link the server printed, or use the code it was given (LEAFEN_SETUP_CODE).")
		return false
	}
	state, address := setupCodeStateOf(event.App), "address:"+requestLimitAddress(event.RealIP())
	if admitted, _ := state.failures.admit(address, time.Now()); !admitted {
		event.Response.Header().Set("Retry-After", "900")
		_ = apiError(event, http.StatusTooManyRequests, errorCodeSetupCodeLimited, "Too many setup codes were tried. Wait a while and try again.")
		return false
	}
	if !setupCodeAccepts(event.App, setupCode) {
		_ = apiError(event, http.StatusForbidden, errorCodeSetupCodeInvalid,
			"Open the setup link the server printed, or use the code it was given (LEAFEN_SETUP_CODE).")
		return false
	}
	state.failures.refund(address)
	return true
}
