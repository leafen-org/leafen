package app

import (
	"errors"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A row page's own share (database_row_page_permissions.go) holds only people
// of its workspace's organization, by id or by an account's email. Refused
// for someone with no account here, or outside the organization, the answer
// goes on with the next way to let them in that the one asking can take
// (2026-11-23: an agent connected with its person's administrator grant read
// "a row's page is shared with people who have one" and told the person to
// have them sign up first): sharing the row's page by their email as any
// page is shared, which lets them in as a guest when they sign up with its
// invitation, and at once for an account an administrator made for them
// first (a share by its id still needs them in the organization); or a guest
// invite link to the page (page_guest_links.go). A
// connection hears only those it can run (native_operation_grant.go), and how
// to get more where it can run none.

// rowShareOutside is such a refusal: noAccount where no account has the
// email, else an account outside the organization.
type rowShareOutside struct {
	refusal   error
	noAccount bool
}

func (outside rowShareOutside) Error() string { return outside.refusal.Error() }
func (outside rowShareOutside) Unwrap() error { return outside.refusal }

// rowShareOutsideRefused refuses a row share for someone it can't hold yet.
func rowShareOutsideRefused(noAccount bool, reason string) error {
	return rowShareOutside{refusal: rowShareRefused("%s", reason), noAccount: noAccount}
}

// rowShareNextWay is one way on, by the operation it runs.
type rowShareNextWay struct {
	method, path string
	// noAccount is a way only for someone with no account yet.
	noAccount bool
	says      string
}

var rowShareNextWays = []rowShareNextWay{
	{method: "POST", path: "/pages/{pageId}/public-mutations",
		says: "share it by their email as any page is shared — POST /pages/%s/public-mutations, action grant_permission, principalType email — and they come in as a guest (with no account yet, when they sign up with its invitation)"},
	{method: "POST", path: "/admin/accounts", noAccount: true,
		says: "make their account first as this server's administrator (POST /admin/accounts), and that share by email lets them in at once"},
	{method: "POST", path: "/workspaces/{workspaceId}/page-guest-links",
		says: "give them a guest invite link to it (POST /workspaces/%s/page-guest-links with pageIds)"},
}

// rowShareNextWaysOf says how to go on from such a refusal, or "".
func rowShareNextWaysOf(event *core.RequestEvent, err error) string {
	var outside rowShareOutside
	if !errors.As(err, &outside) {
		return ""
	}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	workspaceID := "{workspaceId}"
	if row, findErr := event.App.FindRecordById(pagesCollection, rowID); findErr == nil {
		workspaceID = row.GetString("workspace")
	}
	var grant *nativeOperationGrant
	if key, keyed := userKeyOf(event); keyed {
		read, readErr := nativeOperationGrantOf(event.App, key)
		if readErr != nil {
			return ""
		}
		grant = read
	}
	ways := []string{}
	for _, way := range rowShareNextWays {
		if way.noAccount && !outside.noAccount {
			continue
		}
		rule, _ := userKeyRuleOfRoute(way.method, way.path)
		if grant != nil && grant.of(nativeOperation{ID: way.method + " " + way.path, rule: rule}) != nativeOperationCan {
			continue
		}
		value := rowID
		if strings.Contains(way.path, "{workspaceId}") {
			value = workspaceID
		}
		says := way.says
		if strings.Contains(says, "%s") {
			says = strings.Replace(says, "%s", value, 1)
		}
		ways = append(ways, says)
	}
	if len(ways) == 0 {
		return "This connection can take none of the ways on (sharing by email, a guest invite link): " + connectionWiden(grant.kind)
	}
	return "To let them in, " + strings.Join(ways, "; or ") + "."
}
