package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// Where a key's irreversible call asks its person: every route that asks
// does so in its handler, once it checked what it was sent
// (askBeforeActing), so a request it would refuse — a row's page on the page
// route, a page not in the trash, a body that isn't one — is answered at once
// with why, and no one is asked for nothing. A handler whose change is one
// database transaction checks it by making it once and taking it back
// (rehearseBeforeAsking); one whose change goes through a page's content, a
// file, another server or a running app checks first, read only, what that
// change checks, with the same functions. What can be checked only by doing
// it — a backup put together, a password or code, which counts its tries —
// is checked after asking. No route asks before its handler runs any more
// (2026-11-24: a guest link for another workspace's page asked its person
// first and was refused only after their yes): a route that asks is marked
// askedWhereItActs, and its handler asks once it has checked the request.

// userKeyPendingAsk is a call's confirmation still to ask where its handler
// acts.
type userKeyPendingAsk struct {
	authority userKeyAuthority
	rule      userKeyRule
	body      []byte
	asked     bool
}

type userKeyPendingAskKey struct{}

// askedWhereItActs asks, as confirmed does, once the route's handler has
// checked the request and is about to act (askBeforeActing).
func (rule userKeyRule) askedWhereItActs() userKeyRule {
	rule.confirm, rule.asksWhereItActs = true, true
	return rule
}

// askBeforeActing is where a handler that checked its request is about to
// act: a key's call still waiting on its person's yes asks here and is
// answered (true, with the 428 or the approval asked); any other call goes on.
func askBeforeActing(event *core.RequestEvent) (bool, error) {
	pending, ok := event.Request.Context().Value(userKeyPendingAskKey{}).(*userKeyPendingAsk)
	if !ok || pending.asked {
		return false, nil
	}
	pending.asked = true
	return requireUserKeyConfirmation(event, pending.authority, pending.rule, pending.body)
}

// userKeyAsksWhereItActs says the call is a key's still to ask its person
// where its handler acts: a handler that must hold something before it
// rehearses its change (a page's sharing lock) holds it only then.
func userKeyAsksWhereItActs(event *core.RequestEvent) bool {
	pending, ok := event.Request.Context().Value(userKeyPendingAskKey{}).(*userKeyPendingAsk)
	return ok && !pending.asked
}

// errUserKeyAsked is a call askBeforeActing answered, for a handler that
// gives it back through a function: the handler answers nothing more.
var errUserKeyAsked = errors.New("asked its person")

// errUserKeyRehearsed takes back a change made only to see whether it would
// go through.
var errUserKeyRehearsed = errors.New("rehearsed, not kept")

// rehearseBeforeAsking is, for a key's call still to ask its person, what
// the handler's change would refuse: act runs in one transaction that is then
// taken back, so nothing it writes stays and what waits for a commit (the
// …AfterCommit notices) never runs. act changes only the database, through
// the txApp it is given, and checks no secret (a password or a code), whose
// failed tries would be taken back with it. Any other call isn't rehearsed
// (nil); the change itself runs after askBeforeActing, its checks made again.
func rehearseBeforeAsking(event *core.RequestEvent, act func(txApp core.App) error) error {
	if !userKeyAsksWhereItActs(event) {
		return nil
	}
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if err := act(txApp); err != nil {
			return err
		}
		return errUserKeyRehearsed
	})
	if errors.Is(err, errUserKeyRehearsed) {
		return nil
	}
	return err
}

// noteUserKeyActedUnasked logs a confirmed call that went through without
// its handler asking: a route marked askedWhereItActs whose handler never
// calls askBeforeActing.
func noteUserKeyActedUnasked(event *core.RequestEvent, pending *userKeyPendingAsk) {
	if pending == nil || pending.asked || event.Status() >= http.StatusBadRequest {
		return
	}
	event.App.Logger().Error("leafen key call acted without asking where it acts",
		"method", event.Request.Method, "path", event.Request.URL.Path)
}
