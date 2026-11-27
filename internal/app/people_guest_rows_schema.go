package app

// People lists a workspace's guests with an account beside its members
// (readPeopleDirectoryList), and a guest is no record of their own: someone
// becomes one when a page there is shared with them or with an address they
// prove, and stops being one when the last such page goes to the trash, the
// share is taken back, or they join. So that People's automations and an
// open People list follow that moment, as they follow a member joining
// (people_guest_rows.go), People's guests are kept here as last seen
// (leafen_people_guests), and every write that may change who they are —
// a person's share, a shared page's trash, a membership, an address proven
// or not, an organization's standing — marks only its workspace
// (leafen_people_guest_checks) by a trigger, wherever it is written. A
// member who leaves is kept as one ('member') until the look decides
// whether they stay on as a guest, and a guest who leaves stays kept as
// deactivated (people_former_rows_schema.go).

func init() {
	// Both go with their workspace (ON DELETE CASCADE).
	workspaceDeletionDirectAuthorities = append(workspaceDeletionDirectAuthorities,
		workspaceDeletionDirectAuthority{Table: "leafen_people_guest_checks", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
		workspaceDeletionDirectAuthority{Table: "leafen_people_guests", Key: "workspace_id", Disposition: workspaceDeletionAuthorityCascade},
	)
}
