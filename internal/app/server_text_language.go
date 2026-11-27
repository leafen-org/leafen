package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The language Leafen writes and names things in for a person goes by one
// rule, each step taken only where the one before gives nothing (user
// decision 2026-11-27):
//  1. the language the person chose for Leafen;
//  2. for one who follows their device or has not chosen, the language their
//     own request names first (personRequest: a key's request — an agent's,
//     an app's — is its client's, not theirs);
//  3. the language they last read Leafen in on a screen of theirs
//     (person_screen_language.go);
//  4. the server's language: the one its administrator chose as they set it
//     up (serverLanguageCode);
//  5. English.
// What the server writes in Korean and English alone — mail, notices, an
// agent's questions and the names its answers give — reads Korean where that
// language is Korean and English for any other (textLanguageOfCode); what it
// has in every language Leafen speaks — a new database's first words, a
// first workspace — reads that language itself (personLanguageCode). A
// server set up in Korean keeps its people on Korean; on one set up in
// English, or not set, a person nothing else names reads English (it read
// Korean wherever no request was at hand, an English speaker's agent and mail
// too, until 2026-11-27).

// serverTextLanguage is the language, Korean or English, of what the server
// writes for an account: emails, notifications and the pages an email link
// opens, what an agent's answers name and the questions they put to the
// person (mcpReaderLanguage), and the names the server gives what it makes
// for them (generatedNameLanguage); request is the person's own where there
// is one.
func serverTextLanguage(app core.App, userID string, request *http.Request) (string, error) {
	stored, err := storedAccountLanguage(app, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return textLanguageOfCode(languageCodeFor(app, userID, stored.LanguagePreference, requestLanguageCode(personRequest(request)))), nil
}

// personRequest is a request as its person's own: none for one made with a
// key — an agent's or an app's call, leafen_call's inner one, a script's —
// whose languages are its client's, not the person's.
func personRequest(request *http.Request) *http.Request {
	if request == nil {
		return nil
	}
	if _, keyed := request.Context().Value(userKeyContextKey{}).(userKeyAuthority); keyed {
		return nil
	}
	return request
}

// personLanguageCode is the language a person reads Leafen in by the rule,
// by its code, for what comes in every language Leafen speaks.
func personLanguageCode(app core.App, personID string, request *http.Request) string {
	stored, _ := storedAccountLanguage(app, personID)
	return languageCodeFor(app, personID, stored.LanguagePreference, requestLanguageCode(personRequest(request)))
}

// languageCodeFor takes the rule's steps from what is known of a person:
// their stored preference, the language their own request names ("" for
// none) and who they are ("" where no one is known, which skips the last
// screen's language).
func languageCodeFor(app core.App, personID, preference, requested string) string {
	if preference != "" && preference != accountLanguageSystem {
		return preference
	}
	if requested != "" {
		return requested
	}
	if personID != "" {
		if last := lastScreenLanguage(app, personID); last != "" {
			return last
		}
	}
	if server := serverLanguageCode(app); server != "" {
		return server
	}
	return accountLanguageEn
}

// chosenLanguageOrEnglish is a stored preference as a language code where it
// names one, else English: for `leafen setup`'s words, which know the setup
// person's choice alone.
func chosenLanguageOrEnglish(preference string) string {
	if preference == "" || preference == accountLanguageSystem {
		return accountLanguageEn
	}
	return preference
}

// textLanguageOfCode is a language as the server's Korean-and-English texts
// read it.
func textLanguageOfCode(code string) string {
	if code == accountLanguageKo {
		return accountLanguageKo
	}
	return accountLanguageEn
}

// requestTextLanguage is the language, Korean or English, a request whose
// reader isn't known reads the server's texts in: the one it names first,
// else English (requestTextLanguageIn has the server's language before that).
func requestTextLanguage(request *http.Request) string {
	return requestTextLanguageIn(nil, request)
}

// requestTextLanguageIn is requestTextLanguage on a server whose language,
// where its administrator chose one, a request that names none reads.
func requestTextLanguageIn(app core.App, request *http.Request) string {
	if named := requestLanguageCode(request); named != "" {
		return textLanguageOfCode(named)
	}
	if app != nil {
		if server := serverLanguageCode(app); server != "" {
			return textLanguageOfCode(server)
		}
	}
	return accountLanguageEn
}

// requestLanguageCode is the language of Leafen's a request names first, by
// its code: its own where Leafen speaks it (ko-KR is ko, pt-BR pt-BR, zh-TW
// zh-Hant), English for one Leafen doesn't, "" for a request naming none.
func requestLanguageCode(request *http.Request) string {
	if request == nil {
		return ""
	}
	preferred := requestPreferredLanguageTag(request)
	if preferred == "" {
		return ""
	}
	return leafenLanguageOfTag(preferred)
}

// requestPreferredLanguageTag is the language the request names first by
// its Accept-Language weights, lowercased, or empty when it names none.
func requestPreferredLanguageTag(request *http.Request) string {
	preferred, weight := "", 0.0
	for _, part := range strings.Split(request.Header.Get("Accept-Language"), ",") {
		tag, parameters, _ := strings.Cut(strings.TrimSpace(part), ";")
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" || tag == "*" {
			continue
		}
		quality := 1.0
		if value, ok := strings.CutPrefix(strings.TrimSpace(parameters), "q="); ok {
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || parsed > 1 {
				continue
			}
			quality = parsed
		}
		if quality > weight {
			preferred, weight = tag, quality
		}
	}
	return preferred
}
