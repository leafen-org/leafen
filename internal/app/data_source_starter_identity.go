package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database keeps what it was made as wherever it goes, as Notion's
// duplicate keeps its collection's workflow template, type and item name: the
// template it was made from (database_starter_origin.go), its official type
// (notion_database_types.go) and the name it keeps for its pages without a
// title (data_source_untitled_name.go). A duplicated or copied database takes
// them from the one it copies, and an archive carries them, so a copied task
// tracker's untitled rows and templates still read "신규 작업".

// archiveDataSourceStarterIdentity is what an archived data source was made
// as; a database made otherwise carries none.
type archiveDataSourceStarterIdentity struct {
	StarterTemplate string `json:"starterTemplate,omitempty"`
	DatabaseType    string `json:"databaseType,omitempty"`
	UntitledName    string `json:"untitledName,omitempty"`
}

// copyDataSourceStarterIdentity gives each copied data source what the
// source it copies was made as.
func copyDataSourceStarterIdentity(app core.App, jobID string, sourceIDs []string) error {
	if len(sourceIDs) == 0 {
		return nil
	}
	copies, err := pageDuplicationReferenceMappings(app, jobID, "data_source", sourceIDs)
	if err != nil {
		return err
	}
	for _, sourceID := range sourceIDs {
		target := copies[sourceID]
		if target == "" || target == sourceID {
			continue
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_data_sources}} AS [[copy]]
			SET [[starter_template]] = [[original]].[[starter_template]],
				[[database_type]] = [[original]].[[database_type]],
				[[untitled_name]] = [[original]].[[untitled_name]]
			FROM (SELECT [[starter_template]], [[database_type]], [[untitled_name]]
				FROM {{leafen_data_sources}} WHERE [[id]] = {:source}) AS [[original]]
			WHERE [[copy]].[[id]] = {:target}
		`).Bind(dbx.Params{"source": sourceID, "target": target}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// importedDataSourceStarterIdentity keeps what an archive says its data
// source was made as, leaving out a template Leafen doesn't have, a type
// Notion's API doesn't give and a name no title could be.
func importedDataSourceStarterIdentity(value *archiveDataSourceStarterIdentity) *archiveDataSourceStarterIdentity {
	if value == nil {
		return nil
	}
	identity := archiveDataSourceStarterIdentity{}
	if value.StarterTemplate != "" {
		if catalog, err := readDatabaseStarterCatalog(); err == nil {
			for _, template := range catalog.Templates {
				if template.Key == value.StarterTemplate {
					identity.StarterTemplate = value.StarterTemplate
				}
			}
		}
	}
	switch value.DatabaseType {
	case notionDatabaseTypeTasks, notionDatabaseTypeProjects, notionDatabaseTypeMeetingNotes:
		identity.DatabaseType = value.DatabaseType
	}
	if name := strings.TrimSpace(value.UntitledName); name == value.UntitledName && len([]rune(name)) <= maxGraphTitleRunes {
		identity.UntitledName = name
	}
	if identity == (archiveDataSourceStarterIdentity{}) {
		return nil
	}
	return &identity
}

// recordDataSourceStarterIdentity writes what an imported data source was
// made as.
func recordDataSourceStarterIdentity(app core.App, dataSourceID string, identity archiveDataSourceStarterIdentity) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_data_sources}}
		SET [[starter_template]] = {:template}, [[database_type]] = {:type}, [[untitled_name]] = {:name}
		WHERE [[id]] = {:id}
	`).Bind(dbx.Params{
		"id": dataSourceID, "template": identity.StarterTemplate, "type": identity.DatabaseType, "name": identity.UntitledName,
	}).Execute()
	return err
}
