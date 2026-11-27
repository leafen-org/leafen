package app

import "github.com/pocketbase/pocketbase/core"

// checkPageMetadataContent holds a metadata change to the page's content
// rules. What a page is called is its content: a title, icon or cover that
// changes meets the DLP policy however it comes (2026-11-04: an agent's new
// title went past it); a move or trash of what is already there doesn't.
// An icon or cover it changes to an uploaded file is a picture uploaded
// into the workspace (look_picture_files.go).
func checkPageMetadataContent(app core.App, authorization pageAuthorization, page *core.Record, input pageMetadataInput) error {
	changed := []any{}
	if input.Title != page.GetString("title") {
		changed = append(changed, input.Title)
	}
	if input.Icon != nil && *input.Icon != page.GetString("icon") {
		changed = append(changed, *input.Icon)
	}
	if input.Cover != nil && *input.Cover != page.GetString("cover") {
		changed = append(changed, *input.Cover)
	}
	if len(changed) > 0 {
		if err := enforceOrganizationDLPContent(app, authorization.Workspace.Organization.OrganizationID, changed...); err != nil {
			return err
		}
	}
	return checkGivenLookFiles(app, authorization.Workspace.WorkspaceID,
		givenImageIcon("icon", input.Icon, input.IconType, page.GetString("icon")),
		givenCover("cover", input.Cover, page.GetString("cover")),
	)
}
