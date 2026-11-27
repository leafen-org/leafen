package app

import "fmt"

// What a refused page write says to fix (request_refusal.go): the field as
// the request names it and why, after "Check the page details.".

// identifierFieldProblem is why an id the request names field is too long,
// or empty where it is required, or "".
func identifierFieldProblem(field, value string, most int, required bool) string {
	switch {
	case value == "" && required:
		return field + " is empty"
	case len(value) > most:
		return fmt.Sprintf("%s is %d bytes, over %d", field, len(value), most)
	}
	return ""
}

// pageCreationInputProblem is why a page can't be made as asked, or "".
func pageCreationInputProblem(input pageCreationAdmissionInput) string {
	if count := len([]rune(input.RequestID)); input.RequestID == "" || count > maxPageCreationRequestIDLength {
		return fmt.Sprintf("requestId is a new id of yours, 1–%d characters, so the same request sent again makes one page", maxPageCreationRequestIDLength)
	}
	for _, field := range []struct {
		name, value string
		required    bool
	}{
		{"workspaceId", input.WorkspaceID, true}, {"teamspaceId", input.TeamspaceID, false},
		{"parentId", input.ParentID, false}, {"anchorBlockId", input.AnchorBlockID, false},
	} {
		if problem := identifierFieldProblem(field.name, field.value, maxWorkspacePermissionIdentifierLength, field.required); problem != "" {
			return problem
		}
	}
	switch count := len([]rune(input.Title)); {
	case input.PreparedPageID != "" && !validPageRecordID(input.PreparedPageID):
		return fmt.Sprintf("pageId is left out, or an id of up to %d characters without spaces", maxGraphIdentifierLength)
	case input.AnchorBlockID != "" && (input.ParentID == "" || input.BodyPlacement != nil):
		return "anchorBlockId goes with parentId: the block of the parent page the new page follows"
	case count > maxGraphTitleRunes:
		return fmt.Sprintf("title is %d characters, over %d", count, maxGraphTitleRunes)
	case !validPageBodyPlacement(input.BodyPlacement):
		return "the page's place is \"start\" or \"end\", or \"after\" a page or block by its id"
	}
	return ""
}

// pageCursorConflictMessage is a page change refused because the page moved
// on from baseCursor: where its cursor is read, to send again.
const pageCursorConflictMessage = "Load the latest page state again: baseCursor isn't the page's cursor now; GET /pages/{id} reads it (cursor), then send the change again with it."

// pageMetadataInputProblem is why a page's details can't change as asked,
// or "".
func pageMetadataInputProblem(pageID string, input pageMetadataInput) string {
	switch count := len([]rune(input.MutationID)); {
	case input.MutationID == "" || count > 255:
		return "mutationId is a new id of yours, 1–255 characters, so the same change sent again isn't made twice; baseCursor is the page's cursor as GET /pages/{id} reads it"
	case input.BaseCursor < 0:
		return "baseCursor is the page's cursor as GET /pages/{id} reads it"
	case len([]rune(input.Title)) > maxGraphTitleRunes:
		return fmt.Sprintf("title is over %d characters", maxGraphTitleRunes)
	case len(input.ParentID) > maxWorkspacePermissionIdentifierLength:
		return fmt.Sprintf("parentId is %d bytes, over %d", len(input.ParentID), maxWorkspacePermissionIdentifierLength)
	case input.ParentID != "" && input.ParentID == pageID:
		return "parentId is the page itself"
	case input.TeamspaceID != nil && len(*input.TeamspaceID) > maxWorkspacePermissionIdentifierLength:
		return fmt.Sprintf("teamspaceId is %d bytes, over %d", len(*input.TeamspaceID), maxWorkspacePermissionIdentifierLength)
	case !validPageTitleSpansInput(input):
		return graphTitleSpansProblem("titleSpans", *input.TitleSpans, true)
	case !validPagePresentationInput(input):
		return "icon goes with iconType (none with an empty icon; emoji, image, custom_emoji or a built-in icon with its value), and cover with coverPosition 0–100"
	case !validPageStyleMetadataInput(input):
		return "font is default, serif or mono"
	case !validPageCustomizationMetadataInput(input):
		return "backlinksDisplay and pageCommentsDisplay are default, expanded or off; tableOfContentsDisplay and inlineCommentsDisplay as the page's settings menu names them"
	case !validPagePositionInput(input.Position):
		return "position is a finite number"
	}
	return ""
}
