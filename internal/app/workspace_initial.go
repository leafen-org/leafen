package app

import (
	"context"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// startFirstWorkspace makes a first administrator's workspace and its
// welcome page — for an agent's start (user decision 2026-11-13: the first
// run can be done in an agent's conversation alone) and in the setup screen's
// claim (claimFirstPage): what the window's language step makes as it ends
// (AuthenticatedAccountGates' prepareInitialWorkspace), named after the
// person, in the language chosen for Leafen. A window that opens later finds
// it and makes none. It answers the welcome page made. Started with no
// language, or "follow the device", it is named as the server writes to that
// person with no request of theirs (languageCodeFor): an agent's start knows
// no device.
func startFirstWorkspace(app core.App, user *core.Record, language string) (string, error) {
	tag := languageCodeFor(app, user.Id, strings.TrimSpace(language), "")
	template, found := workspaceFirstNames[tag]
	if !found {
		template = workspaceFirstNames[accountLanguageEn]
	}
	name := []rune(strings.ReplaceAll(template, "{{name}}", strings.TrimSpace(user.GetString("name"))))
	if len(name) > maxWorkspaceCreationNameRunes {
		name = name[:maxWorkspaceCreationNameRunes]
	}
	input := workspaceCreateInput{RequestID: "first-run-" + user.Id, Name: strings.TrimSpace(string(name)), EnsureInitialWorkspace: true, Language: tag}
	requestHash, err := workspaceCreationRequestHash(input)
	if err != nil {
		return "", err
	}
	nameLanguage := workspaceNameLanguage(tag)
	welcome := workspaceWelcomeTextFor(tag, nameLanguage)
	var response workspaceCreateResponse
	err = app.RunInTransaction(func(txApp core.App) error {
		var createErr error
		response, _, createErr = createWorkspaceRecords(txApp, user, input, requestHash, welcome, nameLanguage)
		return createErr
	})
	if err != nil || response.Page == nil || response.Duplicate {
		return "", err
	}
	ygo, _ := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	return response.Page.ID, writeWorkspaceWelcomePage(context.Background(), ygo, user.Id, response.Page.ID, welcome)
}
