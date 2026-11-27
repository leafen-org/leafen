package app

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/text/language"
)

// The language a person last read Leafen in on a screen of theirs — what
// their own signed-in request named, as a browser or the apps' web view
// sends it (registerPersonRequestLanguage) — is kept, so what the server
// writes them with no request at hand (mail, notices, an agent's answers)
// reads it where they chose none (server_text_language.go, step 3). It is
// written only when it changes, kept in memory beside the table.

const personScreenLanguageTable = "leafen_person_screen_languages"

// personLanguageMemory is a server's memory of its people's screen
// languages and of its own language, kept in its store: a process may hold
// more than one server.
type personLanguageMemory struct {
	screens sync.Map // person ID → language code ("" known to be none)
	server  atomic.Pointer[heldServerLanguage]
}

type heldServerLanguage struct {
	code  string
	until time.Time
}

const personLanguageMemoryKey = "leafenPersonLanguageMemory"

func personLanguageMemoryOf(app core.App) *personLanguageMemory {
	return app.Store().GetOrSet(personLanguageMemoryKey, func() any { return &personLanguageMemory{} }).(*personLanguageMemory)
}

// lastScreenLanguage is the language a person last read Leafen in on a
// screen of theirs, or "".
func lastScreenLanguage(app core.App, personID string) string {
	memory := personLanguageMemoryOf(app)
	if held, ok := memory.screens.Load(personID); ok {
		return held.(string)
	}
	code := ""
	err := app.DB().NewQuery(`SELECT [[language]] FROM {{` + personScreenLanguageTable + `}} WHERE [[owner_id]] = {:owner}`).
		Bind(dbx.Params{"owner": personID}).Row(&code)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	memory.screens.Store(personID, code)
	return code
}

// rememberScreenLanguage keeps the language a person's own request named,
// when it is another than the one kept: at once in memory, and in the table
// after, so the request doesn't wait on the write.
func rememberScreenLanguage(app core.App, personID, code string) {
	if personID == "" || code == "" || lastScreenLanguage(app, personID) == code {
		return
	}
	memory := personLanguageMemoryOf(app)
	memory.screens.Store(personID, code)
	go storeScreenLanguage(app, memory, personID, code)
}

func storeScreenLanguage(app core.App, memory *personLanguageMemory, personID, code string) {
	if _, err := app.DB().NewQuery(`INSERT INTO {{` + personScreenLanguageTable + `}} ([[owner_id]], [[language]], [[updated_at]])
		VALUES ({:owner}, {:language}, {:now})
		ON CONFLICT ([[owner_id]]) DO UPDATE SET [[language]] = excluded.[[language]], [[updated_at]] = excluded.[[updated_at]]`).
		Bind(dbx.Params{"owner": personID, "language": code, "now": time.Now().UnixMilli()}).Execute(); err != nil {
		memory.screens.Delete(personID)
	}
}

// serverLanguageCode is the server's language: the one its administrator
// chose for themselves as they set it up (saveSetupLanguage), while they keep
// one; "" while none is chosen. It is held a few seconds: it is read for
// every text written for someone who chose no language.
func serverLanguageCode(app core.App) string {
	memory := personLanguageMemoryOf(app)
	if held := memory.server.Load(); held != nil && time.Now().Before(held.until) {
		return held.code
	}
	code := ""
	administrator := ""
	if err := app.DB().NewQuery(`SELECT [[setup_admin_id]] FROM {{leafen_server_settings}} WHERE [[id]] = 'default'`).
		Row(&administrator); err == nil && administrator != "" {
		if stored, err := storedAccountLanguage(app, administrator); err == nil &&
			stored.LanguagePreference != "" && stored.LanguagePreference != accountLanguageSystem {
			code = stored.LanguagePreference
		}
	}
	memory.server.Store(&heldServerLanguage{code: code, until: time.Now().Add(5 * time.Second)})
	return code
}

// forgetServerLanguage has the server's language read again: an account's
// chosen language changed, which may be its administrator's.
func forgetServerLanguage(app core.App) {
	personLanguageMemoryOf(app).server.Store(nil)
}

// leafenLanguages are the languages Leafen speaks, English first: what a
// tag Leafen doesn't speak comes to.
var leafenLanguages = func() []string {
	codes := []string{accountLanguageEn}
	for _, code := range accountLanguagePreferenceValues {
		if code != accountLanguageSystem && code != accountLanguageEn {
			codes = append(codes, code)
		}
	}
	return codes
}()

var leafenLanguageMatcher = func() language.Matcher {
	tags := make([]language.Tag, 0, len(leafenLanguages))
	for _, code := range leafenLanguages {
		tags = append(tags, language.Make(code))
	}
	return language.NewMatcher(tags)
}()

// leafenLanguageOfTag is the language of Leafen's a request's tag names, by
// its code: ko-KR is ko, zh-TW zh-Hant, pt pt-BR; English for one Leafen
// doesn't speak.
func leafenLanguageOfTag(tag string) string {
	parsed, err := language.Parse(strings.TrimSpace(tag))
	if err != nil {
		return accountLanguageEn
	}
	_, index, confidence := leafenLanguageMatcher.Match(parsed)
	if confidence == language.No || index < 0 || index >= len(leafenLanguages) {
		return accountLanguageEn
	}
	return leafenLanguages[index]
}

func ensureNativePersonScreenLanguageSchema(app core.App) error {
	if _, err := app.DB().NewQuery(`CREATE TABLE IF NOT EXISTS {{` + personScreenLanguageTable + `}} (
		[[owner_id]] TEXT PRIMARY KEY NOT NULL,
		[[language]] TEXT NOT NULL CHECK (length([[language]]) BETWEEN 2 AND 16),
		[[updated_at]] INTEGER NOT NULL
	)`).Execute(); err != nil {
		return err
	}
	return validateNativePersonScreenLanguageSchema(app)
}

func validateNativePersonScreenLanguageSchema(app core.App) error {
	var columns int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('` + personScreenLanguageTable + `')
		WHERE [[name]] IN ('owner_id', 'language', 'updated_at')`).Row(&columns); err != nil {
		return err
	}
	if columns != 3 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
