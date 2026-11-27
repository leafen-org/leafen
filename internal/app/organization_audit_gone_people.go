package app

// A person whose account is gone keeps no name on the organization's audit
// lines but their own deletion's, which keeps the email they had then (user
// decision 2026-11-07): the others say "알 수 없는 사용자", as they do for
// whoever did something. A line names a person by account — the one it
// touched, the member a line of the organization's settings or a SCIM update
// was about (metadata's userId), the member a group took or lost
// (metadata's memberId) — and keeps their name or email from then only for
// as long as that account is there (2026-11-13: a removed member's line and a
// group's member line still named someone who had deleted their account).
// The log, its CSV and JSON files and an account's history read lines alike.

// organizationAuditPersonSQL is the account a line is about, if a person.
var organizationAuditPersonSQL = `COALESCE(NULLIF(json_extract({{leafen_organization_audit_events}}.[[metadata_json]], '$.userId'), ''),
	CASE WHEN {{leafen_organization_audit_events}}.[[subject_type]] IN ('user', 'workspace_member', 'workspace_owner', 'organization_owner')
		THEN {{leafen_organization_audit_events}}.[[subject_id]] END)`

// organizationAuditGonePeopleSQL is two columns: whether the person a line is
// about has deleted their account since (never on that deletion's own line),
// and whether the member it names has. A deleted account keeps its record
// with no name and an address no reader sees (deletedAccountEmail).
var organizationAuditGonePeopleSQL = `CASE WHEN {{leafen_organization_audit_events}}.[[action]] = 'user.deleted' THEN 0
		ELSE COALESCE((SELECT 0 FROM {{users}} [[person]] WHERE [[person]].[[id]] = ` + organizationAuditPersonSQL + `
				AND ` + accountKeptSQL("person") + `),
			` + organizationAuditPersonSQL + ` IS NOT NULL) END AS [[person_gone]],
	COALESCE((SELECT 0 FROM {{users}} [[member]]
			WHERE [[member]].[[id]] = json_extract({{leafen_organization_audit_events}}.[[metadata_json]], '$.memberId')
				AND ` + accountKeptSQL("member") + `),
		COALESCE(json_extract({{leafen_organization_audit_events}}.[[metadata_json]], '$.memberId'), '') <> '') AS [[member_gone]]`

// accountKeptSQL is whether an account is still there, not deleted.
func accountKeptSQL(alias string) string {
	return `[[` + alias + `]].[[email]] NOT LIKE '` + deletedAccountEmailPrefix + `%` + deletedAccountEmailSuffix + `'`
}

// organizationAuditPersonKeys are what a line keeps of the person it is about.
var organizationAuditPersonKeys = []string{"name", "email", "previousEmail", "renamedFrom"}

// forgetGoneOrganizationAuditPeople takes a deleted account's name and email
// off a line's metadata; its ids stay, which the screen reads as "알 수 없는
// 사용자".
func forgetGoneOrganizationAuditPeople(metadata map[string]any, personGone, memberGone bool) {
	if personGone {
		for _, key := range organizationAuditPersonKeys {
			delete(metadata, key)
		}
	}
	if memberGone {
		delete(metadata, "member")
	}
}
