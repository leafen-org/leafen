package app

// What an agent asked to change a person's look learns beside their display
// preferences: the look, light, dark or the system's, is each browser's own
// (web/src/lib/theme.ts, as Notion sets it per device), shown in Settings ›
// Preferences beside these preferences but kept by no server, so no call
// sets it (2026-11-25: "dark mode on" finds these preferences, the nearest
// the person's words name).
func init() {
	for _, id := range []string{"GET " + accountDisplayPreferencesPath, "PATCH " + accountDisplayPreferencesPath} {
		nativeOperationNotes[nativeOperationShape(id)] = "The look — light, dark or the system's (the theme, dark mode) — isn't among " +
			"these preferences: each browser keeps its own, which the person picks under Preferences on that device; no call reaches it."
	}
}
