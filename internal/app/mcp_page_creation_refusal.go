package app

// How notion-create-pages and notion-create-database say a page wasn't made.

type mcpPageCreationFailure struct {
	status pageCreationAdmissionStatus
}

func (failure *mcpPageCreationFailure) Error() string {
	return "mcp page creation admission failed"
}

// mcpPageCreationError says why a page wasn't made. A parent the account
// may not add pages under is refused as a permission it lacks
// (LEAFEN_MCP_SCOPE_DENIED), one it views included — not as input to fix
// (2026-11-19: a page shared to view answered TOOL_INVALID "it is a
// full-page database, locked, or being changed"); the tools then say a
// parent the account doesn't open as not found (mcpNamedRefusal). A parent
// no one can add a page to now says which; one in the trash, or under a page
// that is, says to restore it (page_creation_parent_trash.go), as Notion's
// API refuses it as a request to fix (seen 2026-11-20: 400 "…archived…").
func mcpPageCreationError(err error, status pageCreationAdmissionStatus) error {
	if isDLPPolicyDecisionError(err) {
		return mcpDLPError(err)
	}
	if isPermissionDenied(err) || status == pageCreationWorkspaceMissing {
		return mcpDenied("the parent is gone, or this account can't add pages there")
	}
	if status == pageCreationParentDenied {
		return mcpDenied("this account can't add pages under that parent: it may only view it, or doesn't reach it — ask someone who can edit it, or its owner")
	}
	if status == pageCreationParentInTrash {
		return mcpInvalid("the parent can't take a new page: it is in the trash, or under a page that is — restore that page from the trash (notion-restore-pages), then add pages to it")
	}
	if status == pageCreationParentConflict || status == pageCreationIdentityReused {
		return mcpInvalid("the parent can't take a new page: it is a full-page database, or being changed or deleted for good — or this request's ID was used for another")
	}
	// What failed, its cause kept; every refusal of the parent is said above.
	return failedBecause(errMCPToolFailed, err)
}
