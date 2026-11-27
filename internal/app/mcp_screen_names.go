package app

import (
	"context"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// What an agent tells its person to press names Leafen's screens in the
// words of that person's own screen, as the web app's catalogs have them
// (mcp_screen_names.generated.go): 2026-11-15, an agent sent a person whose
// screen says "AI 에이전트 연결" and "설정 → 기능 → 에이전트" to "Connect an AI
// agent" and "Settings → Features → Agents". The rest of an agent's text
// stays in English, which agents read.

// mcpScreenLanguage is the language of a person's screens, by the rule
// (personLanguageCode), where Leafen names its screens in it; else English.
func mcpScreenLanguage(app core.App, personID string) string {
	return mcpScreenLanguageIn(context.Background(), app, personID)
}

// mcpScreenLanguageIn is mcpScreenLanguage for the person whose own request,
// where there is one, ctx carries: a screen of theirs names its screens in
// its language.
func mcpScreenLanguageIn(ctx context.Context, app core.App, personID string) string {
	if code := personLanguageCodeIn(ctx, app, personID); mcpScreenNames[code] != nil {
		return code
	}
	return accountLanguageEn
}

// mcpCallScreenLanguage is mcpScreenLanguage for the person a tool call is
// made for.
func mcpCallScreenLanguage(ctx context.Context, app core.App) string {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return ""
	}
	return mcpScreenLanguage(app, authority.owner())
}

// mcpScreen names a screen, or the way to one, in quotes as the person's
// screen draws it. For a person not known yet — an agent that can't sign in
// — it gives the English names and the Korean ones beside them.
func mcpScreen(language string, names ...string) string {
	if language == "" {
		english, korean := mcpScreen(accountLanguageEn, names...), mcpScreen(accountLanguageKo, names...)
		return english + " (" + korean + " on a Korean screen)"
	}
	labels := make([]string, 0, len(names))
	for _, name := range names {
		label := mcpScreenNames[language][name]
		if label == "" {
			label = mcpScreenNames[accountLanguageEn][name]
		}
		labels = append(labels, label)
	}
	return "“" + strings.Join(labels, " → ") + "”"
}
