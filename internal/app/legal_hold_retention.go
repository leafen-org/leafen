package app

// A retention sweep keeps what an active legal hold covers (the audit
// decision's 2026-10-21 23:06 addition): a hold over all of an organization
// keeps all its records, one over a workspace that workspace's, one over a
// page the records about that page, one over a person what they did and
// what they read. Every sweep asks here, by the record's columns, so a sweep
// made later goes through the same boundary; the organization module
// answers (RetentionHoldSQL).

// legalHoldRecordSQL is a record a sweep would let go, as SQL expressions
// over its row: its organization, its workspace, the pages it is about and
// the people whose doing or reading it is. Outer columns are qualified, as
// the answer is a subquery of its own.
type legalHoldRecordSQL struct {
	Organization string
	Workspace    string
	Pages        []string
	People       []string
}

// legalHoldKeepsSQL is true of a record an active legal hold covers. Without
// the organization module a hold can't be read: while any is active, sweeps
// keep everything.
func legalHoldKeepsSQL(record legalHoldRecordSQL) string {
	if keeps := organizationPolicy.RetentionHoldSQL; keeps != nil {
		return "(" + keeps(record) + ")"
	}
	return `EXISTS (SELECT 1 FROM {{leafen_organization_legal_holds}} WHERE [[status]] = 'active')`
}

// workspaceOrganizationSQL is a workspace's organization in a query.
func workspaceOrganizationSQL(workspace string) string {
	return `(SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = ` + workspace + `)`
}
