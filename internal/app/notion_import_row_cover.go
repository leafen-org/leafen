package app

import (
	"encoding/json"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// A database page's icon and cover come over as an ordinary page's do
// (notion_import_presentation.go): what Leafen stores directly rides among
// the row's staged values, and an image Notion hosts is a file source named
// after the row (#page-icon, #page-cover), copied with the import's other
// files.

// notionImportRowCoverKey carries a row's external cover among its staged
// values; no property source ID has its form.
const notionImportRowCoverKey = "leafen:row-cover"

// stageNotionImportRowPresentation puts among a row's staged values the icon
// and the cover Leafen stores directly.
func stageNotionImportRowPresentation(values map[string]notionDatabaseRowValuePlan, row notionProviderObject) {
	if icon := notionImportPresentationValue(row.Icon, "emoji", "external", nativeIconType); icon != nil {
		values[notionImportRowIconKey] = notionDatabaseRowValuePlan{Type: "icon", Value: icon}
	}
	if cover := notionImportPresentationValue(row.Cover, "external"); cover != nil {
		values[notionImportRowCoverKey] = notionDatabaseRowValuePlan{Type: "cover", Value: cover}
	}
}

// notionImportRowCover takes a row's cover out of its staged values and
// returns the cover the imported row takes: a copied hosted image, or the
// external one Leafen stores directly; "" for none.
func notionImportRowCover(
	app core.App, job storedJob, rowSourceID string, values map[string]notionDatabaseRowValuePlan,
) (string, error) {
	cover := ""
	if stored, found := values[notionImportRowCoverKey]; found {
		delete(values, notionImportRowCoverKey)
		encoded, err := json.Marshal(stored.Value)
		if err != nil {
			return "", err
		}
		if presentation, err := notionPagePresentation(nil, encoded); err == nil && presentation.Cover != nil &&
			!presentation.CoverManaged {
			cover = *presentation.Cover
		}
	}
	fileID, err := notionImportCopiedPresentationFile(app, job.ID, job.WorkspaceID, rowSourceID+notionPresentationPageCover)
	if err != nil {
		return "", err
	}
	if fileID != "" {
		cover = fileID
	}
	return cover, nil
}

// applyNotionImportRowCover sets an imported row's cover on its page record,
// by the person importing or recovering it, as a cover set through the API
// is (notion_database_row_page_presentation.go), with its copied image kept
// by the page.
func applyNotionImportRowCover(app core.App, rowID, workspaceID, actorID, cover string) error {
	found, err := setRowPageRecordCover(app, rowID, workspaceID, actorID, cover, nil)
	if err != nil {
		return err
	}
	if !found {
		return errors.New(errorCodeNotionImportCreateFailed)
	}
	return replaceProductFilePageReferences(app, workspaceID, rowID, "", "none", cover)
}
