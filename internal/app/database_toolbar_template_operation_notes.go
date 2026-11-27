package app

// What the operations list says beside a database's template routes: the
// read gives a template's revision as templateAuthorityRevision and the
// changes take it as expectedRevision (expectedTemplateRevision for a row
// made from one), and a copy names two templates (2026-11-24, an agent's real
// use: duplicateTemplate's expectedRevision had to be found under another
// name, and templateId and targetTemplateId told apart from refusals).

const (
	databaseToolbarTemplatesRead = "GET /pages/{pageId}/databases/{dataSourceId}/toolbar-config"
	databaseToolbarTemplateNames = "templateId names a template by the id this read lists, expectedRevision is its " +
		"templateAuthorityRevision there, and expectedWorkspaceContentRevision is the read's workspaceContentRevision."
)

func init() {
	for id, note := range map[string]string{
		databaseToolbarTemplatesRead: "Lists the database's templates, each with its id and templateAuthorityRevision, which its " +
			"changes (POST …/toolbar-config/mutations) name: " + databaseToolbarTemplateNames + " A template's own address, " +
			"which notion-fetch reads and notion-create-pages takes as template_id, is /p/{its id}.",
		"POST /pages/{pageId}/databases/{dataSourceId}/toolbar-config/mutations": "As GET …/toolbar-config lists them: " +
			databaseToolbarTemplateNames + " createTemplate takes a new templateId of yours and no expectedRevision. " +
			"duplicateTemplate copies the template templateId names into a new one whose id is targetTemplateId, a new id of yours; " +
			"expectedRevision is the copied template's.",
		"POST /pages/{pageId}/databases/{dataSourceId}/toolbar-config/template-rows": "Makes a row from a template: templateId " +
			"and expectedTemplateRevision are its id and templateAuthorityRevision as GET …/toolbar-config lists them.",
	} {
		nativeOperationNotes[nativeOperationShape(id)] = note
	}
}
