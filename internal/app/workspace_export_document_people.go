package app

// personName is how a readable export names a person a value or a comment
// names: their name now (loadPeople), or, for one the workspace doesn't know
// — someone gone from it, or another workspace's connection bot an archive
// brought along — the words the app shows for them (peopleDirectory's
// "unknown"), never their internal ID.
func (document *workspaceExportDocument) personName(id string) string {
	if name := document.People[id]; name != "" {
		return name
	}
	if document.Language == accountLanguageKo {
		return "알 수 없는 사용자"
	}
	return "Unknown user"
}
