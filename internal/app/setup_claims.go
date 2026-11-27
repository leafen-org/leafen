package app

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

// setupClaims knows which setup screen's claim set the server up. A setup
// screen asks every two seconds whether the server was set up elsewhere
// (POST /setup/agent-start), and its own claim sets it up too: the answer
// names the asking screen's own claim as its own (2026-11-19: a screen took
// its own claim's "set up" for another place's and signed its first
// administrator out before their sign-in was kept). The claim's id counts
// from before its transaction, so no moment of the server being set up goes
// unclaimed; the server keeps it while it runs, as long as a screen asks.
//
// An agent's start (`leafen setup start`, first_run_person_ask.go) sets the
// server up in steps — the account, its first workspace, then the one sign-in
// left for the screen holding the setup code — and no screen hears the server
// is set up until the last of them is done: a screen told in between went to
// the sign-in with no sign-in left for it.
type setupClaims struct {
	mu       sync.Mutex
	by       string
	pending  map[string]int
	starting int
}

const (
	setupClaimsStoreKey = "leafenSetupClaims"
	setupClaimIDRunes   = 128
)

func setupClaimsOf(app core.App) *setupClaims {
	claims, _ := app.Store().GetOrSet(setupClaimsStoreKey, func() any {
		return &setupClaims{pending: map[string]int{}}
	}).(*setupClaims)
	return claims
}

// setupClaimID is a screen's claim id as it sent it, or none.
func setupClaimID(id string) string {
	id = strings.TrimSpace(id)
	if utf8.RuneCountInString(id) > setupClaimIDRunes {
		return ""
	}
	return id
}

// begin counts a claim under way; the returned end says whether it set the
// server up.
func (claims *setupClaims) begin(id string) func(setUp bool) {
	id = setupClaimID(id)
	if id == "" {
		return func(bool) {}
	}
	claims.mu.Lock()
	claims.pending[id]++
	claims.mu.Unlock()
	return func(setUp bool) {
		claims.mu.Lock()
		defer claims.mu.Unlock()
		if claims.pending[id]--; claims.pending[id] <= 0 {
			delete(claims.pending, id)
		}
		if setUp {
			claims.by = id
		}
	}
}

// own says whether the claim with this id set the server up, or is setting
// it up now.
func (claims *setupClaims) own(id string) bool {
	id = setupClaimID(id)
	if id == "" {
		return false
	}
	claims.mu.Lock()
	defer claims.mu.Unlock()
	return claims.by == id || (claims.by == "" && claims.pending[id] > 0)
}

// agentStart counts an agent's start under way, until the returned end.
func (claims *setupClaims) agentStart() func() {
	claims.mu.Lock()
	claims.starting++
	claims.mu.Unlock()
	return func() {
		claims.mu.Lock()
		claims.starting--
		claims.mu.Unlock()
	}
}

// agentStarting says whether an agent's start is still setting the server up.
func (claims *setupClaims) agentStarting() bool {
	claims.mu.Lock()
	defer claims.mu.Unlock()
	return claims.starting > 0
}
