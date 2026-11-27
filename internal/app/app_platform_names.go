package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The names an app's data takes where people see them — its workspace, its
// account, its teamspaces and pages — are the app's name in the language
// the people read: a person's own pages in that person's, what an
// organization shares in its owner's.

// appNameFor is the app's name as an account reads it: in the language it
// chose when the app names itself in that language, else in the language of
// the texts Leafen writes for it, else the app's default name.
func appNameFor(app core.App, manifest appManifest, userID string) string {
	if stored, err := storedAccountLanguage(app, userID); err == nil {
		if name := appNameIn(manifest, stored.LanguagePreference); name != "" {
			return name
		}
	}
	if language, err := serverTextLanguage(app, userID, nil); err == nil {
		if name := appNameIn(manifest, language); name != "" {
			return name
		}
	}
	return appDisplayName(manifest)
}

// appOrganizationName is the app's name for what it makes for a tenancy:
// as an organization's owner reads it, or by the server's text language for
// the server's own tenancy.
func appOrganizationName(app core.App, manifest appManifest, tenancyKey string) string {
	owner := ""
	if organizationID, ok := strings.CutPrefix(tenancyKey, "org:"); ok && tenancyKey != appTenancyServer {
		if organization, err := app.FindRecordById(organizationsCollection, organizationID); err == nil {
			owner = organization.GetString("owner")
		}
	}
	return appNameFor(app, manifest, owner)
}

// appNameIn is the app's name in a language, or in its primary language
// ("zh" for "zh-TW"), or empty.
func appNameIn(manifest appManifest, language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" {
		return ""
	}
	for locale, name := range manifest.Name {
		if strings.ToLower(locale) == language && name != "" {
			return name
		}
	}
	primary, _, _ := strings.Cut(language, "-")
	return manifest.Name[primary]
}

// appPlaceTitles names what an app makes in each place by what it holds and
// what people may do there (2026-10-16: the teamspaces and root pages all
// took the app's name, so the view-only, editable and person's own data
// couldn't be told apart). The workspace and the editable teamspace keep the
// app's name.
func appPlaceTitles(name, language string) (viewTeamspace string, roots map[string]string) {
	if language == accountLanguageKo {
		return name + " · 보기 전용", map[string]string{
			appDataPlaceEdit: "함께 사용하는 데이터", appDataPlaceView: "앱이 관리하는 데이터", appDataPlaceHidden: name, appDataPlacePerson: name + " · 내 데이터",
		}
	}
	return name + " · view only", map[string]string{
		appDataPlaceEdit: "Shared data", appDataPlaceView: "Data the app keeps", appDataPlaceHidden: name, appDataPlacePerson: name + " · my data",
	}
}

// appOrganizationLanguage is the language Leafen writes in for what an app
// makes for a tenancy: its organization owner's.
func appOrganizationLanguage(app core.App, tenancyKey string) string {
	owner := ""
	if organizationID, ok := strings.CutPrefix(tenancyKey, "org:"); ok && tenancyKey != appTenancyServer {
		if organization, err := app.FindRecordById(organizationsCollection, organizationID); err == nil {
			owner = organization.GetString("owner")
		}
	}
	language, err := serverTextLanguage(app, owner, nil)
	if err != nil {
		return accountLanguageKo
	}
	return language
}
