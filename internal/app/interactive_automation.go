package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

const interactiveAutomationMaxBodyBytes = interactiveAutomationMaxDocumentBytes + 4096

func registerInteractiveAutomationRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence) {
	event.Router.POST(
		apiRoot+interactiveAutomationPageButtonExecutePath,
		func(request *core.RequestEvent) error { return executePageButton(request, ygo) },
	).Bind(apis.BodyLimit(interactiveAutomationMaxBodyBytes))
	event.Router.POST(
		apiRoot+interactiveAutomationLegacyTemplateExecutePath,
		func(request *core.RequestEvent) error { return executeLegacyTemplate(request, ygo) },
	).Bind(apis.BodyLimit(interactiveAutomationMaxBodyBytes))
	event.Router.POST(
		apiRoot+interactiveAutomationDatabaseButtonExecutePath,
		func(request *core.RequestEvent) error { return executeDatabaseButton(request, ygo) },
	).Bind(apis.BodyLimit(interactiveAutomationMaxBodyBytes))
	event.Router.GET(apiRoot+interactiveAutomationTargetDatabaseReadPath, readInteractiveAutomationTargetDatabase)
}

func executeLegacyTemplate(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	return executeInteractiveAutomationButton(event, ygo, interactiveAutomationSourceLocator{
		Kind:    interactiveAutomationSourceLegacyTemplate,
		PageID:  strings.TrimSpace(event.Request.PathValue("pageId")),
		BlockID: strings.TrimSpace(event.Request.PathValue("blockId")),
	})
}

func executePageButton(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	return executeInteractiveAutomationButton(event, ygo, interactiveAutomationSourceLocator{
		Kind:    interactiveAutomationSourcePageButton,
		PageID:  strings.TrimSpace(event.Request.PathValue("pageId")),
		BlockID: strings.TrimSpace(event.Request.PathValue("blockId")),
	})
}

func executeDatabaseButton(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	return executeInteractiveAutomationButton(event, ygo, interactiveAutomationSourceLocator{
		Kind:       interactiveAutomationSourceDatabaseButton,
		PageID:     strings.TrimSpace(event.Request.PathValue("pageId")),
		DatabaseID: strings.TrimSpace(event.Request.PathValue("databaseId")),
		RowID:      strings.TrimSpace(event.Request.PathValue("rowId")),
		PropertyID: strings.TrimSpace(event.Request.PathValue("propertyId")),
	})
}
