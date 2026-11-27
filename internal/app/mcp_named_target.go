package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A tool given a page or database by its ID or address that the account it
// works as doesn't open — it isn't there, it was deleted, or the account
// can't open it — answers that it found none, under LEAFEN_PAGE_NOT_FOUND
// and naming what Notion's tools name (seen 2026-11-16: the kind, the ID,
// and that access and the workspace are worth checking), as notion-fetch answers
// (mcp_tool_fetch.go) and as the app's own requests answer a page someone
// can't open. What is no ID or address at all is refused as input, in the
// words Notion's tools refuse such a reference with (seen 2026-11-10). Told
// LEAFEN_MCP_SCOPE_DENIED "this account can't change it", an agent took a
// page_id its shell had joined from two IDs for a permission it lacked, and
// stopped (2026-11-19). A page the account opens keeps the refusal its tool
// gave: one it reads but can't change is refused as such, and one outside
// what the connection was given says so.

// mcpNotFound is a tool finding no kind (page, database) with the ID or
// address written, named by its ID as written (an address's, its ID);
// written as no ID or address could be, it is refused as input.
func mcpNotFound(kind, written string) error {
	written = strings.TrimSpace(written)
	if !mcpWrittenAsReference(written) {
		return mcpInvalidReference(written)
	}
	return fmt.Errorf("%w: There's no %s with ID: %s that you can reach here; check that you can open it "+
		"and that this is its workspace.", errMCPPageMissing, kind, mcpReferenceID(written))
}

// mcpInvalidReference is a reference refused as no ID or address.
func mcpInvalidReference(written string) error {
	return mcpInvalid("%q is neither a full URL nor an ID or short form this can read", strings.TrimSpace(written))
}

// mcpWrittenAsReference says whether what was written names something as
// Leafen's tools and Notion's give it: an ID — a UUID, with its dashes or
// without them, or a fifteen-character Leafen ID — alone, at the end of an
// address (/p/…, or a slug ending in one), or after a scheme
// (collection://…).
func mcpWrittenAsReference(written string) bool {
	id := mcpReferenceID(written)
	if _, rest, scheme := strings.Cut(id, "://"); scheme {
		id = strings.Trim(rest, "/")
	}
	if end := strings.IndexAny(id, "?#"); end >= 0 {
		id = id[:end]
	}
	if validNotionCompatibleInternalID(id) || mcpUUIDShaped(id) {
		return true
	}
	// A slugged address, Title-<32 hexadecimal digits>.
	at := strings.LastIndex(id, "-")
	return at >= 0 && len(id)-at-1 == 32 && mcpUUIDShaped(id[at+1:])
}

// mcpUUIDShaped says whether id is a UUID: 32 hexadecimal digits, in either
// case, with the four dashes or without them.
func mcpUUIDShaped(id string) bool {
	if len(id) == 36 {
		if id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
			return false
		}
		id = strings.ReplaceAll(id, "-", "")
	}
	if len(id) != 32 {
		return false
	}
	for _, character := range id {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}

// mcpNamedTarget is a page or database a tool was given, as the agent wrote
// it; Kind is "page" or "database".
type mcpNamedTarget struct{ Kind, Written string }

// mcpPage, mcpDatabase and mcpPages name what a tool was given.
func mcpPage(written string) mcpNamedTarget {
	return mcpNamedTarget{Kind: "page", Written: written}
}

func mcpDatabase(written string) mcpNamedTarget {
	return mcpNamedTarget{Kind: "database", Written: written}
}

func mcpPages(written []string) []mcpNamedTarget {
	targets := make([]mcpNamedTarget, 0, len(written))
	for _, each := range written {
		targets = append(targets, mcpPage(each))
	}
	return targets
}

// mcpNamedRefusal is a tool's answer err, a refusal of what it reached
// (LEAFEN_MCP_SCOPE_DENIED) said, for the first of the targets it was given
// that an operation holds for a while, as busy (mcpPageBusy), or that the
// account doesn't open, as mcpNotFound; any other answer, and a refusal whose
// targets it all opens, none held, is as it was.
func mcpNamedRefusal(ctx context.Context, app core.App, err error, targets ...mcpNamedTarget) error {
	if err == nil || !errors.Is(err, errMCPScopeDenied) {
		return err
	}
	authority, authorityErr := mcpAuthorityFromContext(ctx)
	if authorityErr != nil {
		return err
	}
	for _, target := range targets {
		if strings.TrimSpace(target.Written) == "" {
			continue
		}
		pageID, opens := mcpAccountOpensPage(app, authority, target)
		// Held by an operation for a while, it is busy: neither gone nor out
		// of the account's reach (mcp_page_held.go).
		if busy := mcpPageHeld(app, authority, pageID); busy != "" {
			return mcpPageBusy(busy)
		}
		if opens {
			continue
		}
		return mcpNotFound(target.Kind, target.Written)
	}
	return err
}

// mcpAccountOpens says whether the account a connection works as opens a
// target, as fetch would read it: a page it reads (authorizeMCPPageRead), a
// database or data source by the page holding it, the trash's too. When it
// can't tell, it opens, and the tool's refusal stands.
func mcpAccountOpens(app core.App, authority mcpAuthority, target mcpNamedTarget) bool {
	_, opens := mcpAccountOpensPage(app, authority, target)
	return opens
}

// mcpAccountOpensPage is mcpAccountOpens with the page it asked about.
func mcpAccountOpensPage(app core.App, authority mcpAuthority, target mcpNamedTarget) (string, bool) {
	id := mcpLocalID(app, strings.TrimSpace(target.Written))
	if sourceID, ok := parseMCPDataSourceURL(id); ok {
		location, err := loadMCPDataSourceLocationToRead(app, sourceID)
		if err != nil {
			return "", !errors.Is(err, sql.ErrNoRows)
		}
		id = location.PageID
	} else if containerID, found := resolveMCPDatabaseID(app, id); found || mcpDatabaseContainerExistsToRead(app, id) {
		if !found {
			containerID = id
		}
		location, err := loadMCPDatabaseContainerLocationWith(app, containerID, true)
		if err != nil {
			return "", true
		}
		id = location.PageID
	}
	_, err := authorizeMCPPageRead(app, authority.PrincipalID, id)
	return id, !isPermissionDenied(err)
}

// authorizeMCPPageRead opens a page for reading as fetch does: a page under
// its permission, a template as its editor opens it, and a database page
// whose data source is in the trash through the page holding its database
// (mcp_fetch_trash.go).
func authorizeMCPPageRead(app core.App, principalID, pageID string) (pageAuthorization, error) {
	authorization, err := authorizePage(app, principalID, pageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		if template, templateErr := isDatabaseTemplatePageID(app, pageID); templateErr == nil && template {
			authorization, err = authorizeDatabaseTemplateBody(app, principalID, pageID, pageAccessRoleView)
		}
	}
	if isPermissionDenied(err) {
		if row, found, rowErr := authorizeMCPRowInTrashedSource(app, principalID, pageID); rowErr != nil && !isPermissionDenied(rowErr) {
			err = rowErr
		} else if found && rowErr == nil {
			authorization, err = row, nil
		}
	}
	return authorization, err
}
