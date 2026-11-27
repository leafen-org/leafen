package app

// An organization's guest is in none of its workspaces, as a Notion guest
// is: the memberships a person kept after their role turned guest, before a
// role change took them away, go, while the pages shared with them stay
// shared. A workspace's owner keeps theirs until the workspace passes on.
// The workspaces' permission cursors move on so their readers learn it.

const (
	organizationGuestMembershipsVersion = 323
	organizationGuestMembershipsName    = "native_organization_guest_memberships"
)
