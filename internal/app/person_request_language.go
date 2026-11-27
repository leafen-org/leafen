package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// A person's own request — a screen's, as a browser or the apps' web view
// sends it — names the language their device reads. The work it does, however
// deep it writes a text or gives a name for them, reads that language from
// its context (serverTextLanguageIn), so one following their device reads
// one language on their screen: an English screen got English names but a
// Korean word for its untitled rows, its dates searched and a form's copy
// (2026-11-27). A key's request is an agent's or an app's, not the person's
// (personRequest), and work with no request — mail, notices, jobs — has none;
// both read as serverTextLanguage says with no request.

type requestLanguageKey struct{}

// requestLanguage is the language a request named, and the person whose own
// request it was; "" when the caller that put it there makes things for the
// request's own actor (withRequestLanguage).
type requestLanguage struct{ person, language string }

// withRequestLanguage carries the language a person's own request names
// first (requestLanguageCode) to what it makes for its actor. A key's
// request carries none, nor one naming no language.
func withRequestLanguage(ctx context.Context, request *http.Request) context.Context {
	return withPersonRequestLanguage(ctx, "", request)
}

// withPersonRequestLanguage is withRequestLanguage for the person whose own
// request it is: only what is written or named for them reads it.
func withPersonRequestLanguage(ctx context.Context, person string, request *http.Request) context.Context {
	named := requestLanguageCode(personRequest(request))
	if named == "" {
		return ctx
	}
	return context.WithValue(ctx, requestLanguageKey{}, requestLanguage{person: person, language: named})
}

// requestLanguageFor is the language the person's own request in ctx names,
// if ctx carries one for them.
func requestLanguageFor(ctx context.Context, person string) (string, bool) {
	if ctx == nil {
		return "", false
	}
	switch held := ctx.Value(requestLanguageKey{}).(type) {
	case requestLanguage:
		if held.person == "" || held.person == person {
			return held.language, true
		}
	case string:
		return held, true
	}
	return "", false
}

// registerPersonRequestLanguage has every signed-in request carry its
// person's language (withPersonRequestLanguage) to the work it does, and
// keeps it as the language they last read Leafen in on a screen
// (rememberScreenLanguage).
func registerPersonRequestLanguage(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "leafenPersonRequestLanguage", Priority: apis.DefaultLoadAuthTokenMiddlewarePriority + 4,
		Func: func(e *core.RequestEvent) error {
			if e.Auth != nil {
				e.Request = e.Request.WithContext(withPersonRequestLanguage(e.Request.Context(), e.Auth.Id, e.Request))
				if named, ok := requestLanguageFor(e.Request.Context(), e.Auth.Id); ok {
					rememberScreenLanguage(e.App, e.Auth.Id, named)
				}
			}
			return e.Next()
		},
	})
}

// serverTextLanguageIn is serverTextLanguage for the person whose own
// request, where there is one, ctx carries (server_text_language.go).
func serverTextLanguageIn(ctx context.Context, app core.App, userID string) (string, error) {
	stored, err := storedAccountLanguage(app, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	requested, _ := requestLanguageFor(ctx, userID)
	return textLanguageOfCode(languageCodeFor(app, userID, stored.LanguagePreference, requested)), nil
}

// personLanguageCodeIn is personLanguageCode for the person whose own
// request, where there is one, ctx carries.
func personLanguageCodeIn(ctx context.Context, app core.App, personID string) string {
	stored, _ := storedAccountLanguage(app, personID)
	requested, _ := requestLanguageFor(ctx, personID)
	return languageCodeFor(app, personID, stored.LanguagePreference, requested)
}
