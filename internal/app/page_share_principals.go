package app

import (
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// pageSharePrincipals are the ways a page share names a person in a
// workspace: the person, the workspace's groups they belong to (however
// many, matched by the database), their verified email, and the whole
// workspace when a share with it reaches them (WorkspaceMember).
type pageSharePrincipals struct {
	UserID string
	Emails []string
	// A share with "<workspace>의 모든 사용자" reaches its owner and its
	// members, not a restricted member: Notion's restricted member gets
	// "이 페이지에 대한 사용 권한 없음" on a page whose general access is
	// "<workspace>의 모든 사용자 — 읽기 허용", which their search doesn't
	// find either (seen 2026-11-16 in Chrome, a free workspace's restricted
	// member, in two tabs at two times). It is decided here and in
	// workspaceShareMemberSQL, which every list of what a share with the
	// workspace opens reads.
	WorkspaceMember bool
}

// workspaceShareMemberSQL is an SQL expression, true when the user the
// second expression names is one of the members of the workspace the first
// names whom a share with the whole workspace reaches: a member who isn't
// restricted (pageSharePrincipals.WorkspaceMember). Its owner is reached as
// its owner.
func workspaceShareMemberSQL(workspaceExpression, userExpression string) string {
	return "(EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[share_member]]" +
		" WHERE [[share_member]].[[workspace_id]] = " + workspaceExpression +
		" AND [[share_member]].[[user_id]] = " + userExpression + " AND [[share_member]].[[role]] = 'member')" +
		" AND NOT " + workspaceRestrictedMemberSQL(workspaceExpression, userExpression) + ")"
}

type pageSharePrincipalsMemoKey struct{ workspaceID, principalID string }

// loadPageSharePrincipals reads them once a read scope: a member's search
// read their emails again for each page it judged (2026-10-17).
func loadPageSharePrincipals(app core.App, workspaceID, principalID string) (pageSharePrincipals, error) {
	return remembered(app, pageSharePrincipalsMemoKey{workspaceID, principalID}, func() (pageSharePrincipals, error) {
		return readPageSharePrincipals(app, workspaceID, principalID)
	})
}

func readPageSharePrincipals(app core.App, workspaceID, principalID string) (pageSharePrincipals, error) {
	principals := pageSharePrincipals{UserID: principalID}
	member, memberErr := authorizeWorkspace(app, principalID, workspaceID, permissionWorkspaceRead)
	if memberErr != nil && !isPermissionDenied(memberErr) {
		return pageSharePrincipals{}, memberErr
	}
	principals.WorkspaceMember = memberErr == nil && !member.Restricted
	email, err := verifiedAccountEmails(app, principalID)
	if err != nil {
		return pageSharePrincipals{}, err
	}
	principals.Emails = email
	return principals, nil
}

// normalizePageShareEmail is the form an email share and an account's
// email are compared in.
func normalizePageShareEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// condition is the SQL condition that the share row named by alias, in the
// workspace bound as workspaceParam, names one of the principals. It adds
// its values to params.
func (principals pageSharePrincipals) condition(alias, workspaceParam string, params dbx.Params) string {
	workspace := alias + ".[[workspace_id]]={:" + workspaceParam + "}"
	params["shareUser"] = principals.UserID
	terms := []string{"(" + workspace + " AND " + alias + ".[[principal_type]]='user' AND " +
		alias + ".[[principal_id]]={:shareUser})"}
	if principals.WorkspaceMember {
		terms = append(terms, "("+workspace+" AND "+alias+".[[principal_type]]='workspace' AND "+alias+".[[principal_id]]={:"+workspaceParam+"})")
	}
	terms = append(terms, "("+workspace+" AND "+alias+".[[principal_type]]='group' AND "+
		alias+".[[principal_id]] IN (SELECT [[share_group_member]].[[group_id]] "+
		"FROM {{leafen_workspace_permission_group_members}} [[share_group_member]] "+
		"JOIN {{leafen_workspace_permission_groups}} [[share_group]] ON [[share_group]].[[id]]=[[share_group_member]].[[group_id]] "+
		"WHERE [[share_group_member]].[[user_id]]={:shareUser} AND [[share_group]].[[workspace_id]]={:"+workspaceParam+"} "+
		"AND [[share_group]].[[deleted_at]] IS NULL))")
	if len(principals.Emails) > 0 {
		placeholders := make([]string, 0, len(principals.Emails))
		for index, email := range principals.Emails {
			key := fmt.Sprintf("shareEmail%d", index)
			params[key] = email
			placeholders = append(placeholders, "{:"+key+"}")
		}
		terms = append(terms, "("+workspace+" AND "+alias+".[[principal_type]]='email' AND "+
			"lower(trim("+alias+".[[principal_id]])) IN ("+joinSQL(placeholders)+"))")
	}
	return "(" + strings.Join(terms, " OR ") + ")"
}
