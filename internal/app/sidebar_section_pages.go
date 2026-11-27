package app

import (
	"cmp"
	"context"
	"slices"

	"github.com/pocketbase/pocketbase/core"
)

// The sidebar's Private and Shared sections as the server reads them, for a
// reader without the screen (an agent's notion-list-private-pages and
// notion-list-shared-pages): from the same page reads the screen's sidebar
// starts from (bootstrap.go, read whole by readReaderBootstrapPages), by the
// rule its page tree applies to them (web pageTreeModel: privateRootPages
// and sharedSectionPages):
//
//   - a page is in the tree unless it is in the trash, archived, a button's
//     template page or one hanging on a table cell or a caption;
//   - Private heads are the reader's pages at the top of a workspace, outside
//     every teamspace, that aren't shared with them;
//   - Shared heads are the pages shared with the reader whose parent they
//     don't see, less those everyone in the workspace can open, which the
//     section leaves to search.
//
// Each section is in the order the person's sidebar sorts it (by hand, or the
// last edited first).

// sidebarSectionHeads are a workspace's Private and Shared heads.
type sidebarSectionHeads struct {
	Private []pageResponse
	Shared  []pageResponse
}

// readSidebarSectionHeads reads the reader's Private and Shared heads in each
// workspace they're in, by workspace.
func readSidebarSectionHeads(ctx context.Context, app core.App, principalID string) (map[string]sidebarSectionHeads, error) {
	response, err := readReaderBootstrapPages(ctx, app, principalID)
	if err != nil {
		return nil, err
	}
	return sidebarSectionHeadsOf(response.Pages, response.SharedPageIDs, response.WorkspaceWidePageIDs), nil
}

// sidebarSectionHeadsOf applies the sections' rule to the pages a reader
// sees, in the order their pages are put in by hand.
func sidebarSectionHeadsOf(pages []pageResponse, sharedIDs, workspaceWideIDs []string) map[string]sidebarSectionHeads {
	shared := make(map[string]bool, len(sharedIDs))
	for _, id := range sharedIDs {
		shared[id] = true
	}
	wide := make(map[string]bool, len(workspaceWideIDs))
	for _, id := range workspaceWideIDs {
		wide[id] = true
	}
	visible := make(map[string]bool, len(pages))
	for _, page := range pages {
		visible[page.ID] = true
	}
	inTree := func(page pageResponse) bool {
		return !page.InTrash && !page.ButtonTemplate && page.ArchivedAt == nil && !page.FieldAnchored && !page.AnchorUnmentioned
	}
	heads := map[string]sidebarSectionHeads{}
	for _, page := range pages {
		if !inTree(page) {
			continue
		}
		workspace := heads[page.WorkspaceID]
		switch {
		case shared[page.ID]:
			if (page.ParentID == "" || !visible[page.ParentID]) && !wide[page.ID] {
				workspace.Shared = append(workspace.Shared, page)
			}
		case page.ParentID == "" && page.TeamspaceID == "":
			workspace.Private = append(workspace.Private, page)
		}
		heads[page.WorkspaceID] = workspace
	}
	byHand := func(left, right pageResponse) int {
		return cmp.Or(cmp.Compare(left.Position, right.Position), cmp.Compare(left.ID, right.ID))
	}
	for workspaceID, workspace := range heads {
		slices.SortFunc(workspace.Private, byHand)
		slices.SortFunc(workspace.Shared, byHand)
		heads[workspaceID] = workspace
	}
	return heads
}

// sortSidebarSection orders a section's pages as the sidebar sorts it: by
// hand as they are, or the last edited first.
func sortSidebarSection(pages []pageResponse, sort string) []pageResponse {
	if sort != sidebarSectionSortEdited {
		return pages
	}
	sorted := slices.Clone(pages)
	slices.SortStableFunc(sorted, func(left, right pageResponse) int {
		// Stored times are written alike, so they sort as text.
		return cmp.Compare(right.UpdatedAt, left.UpdatedAt)
	})
	return sorted
}
