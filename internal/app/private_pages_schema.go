package app

// A page outside every teamspace is private, as in Notion: its tree belongs
// to one person, who sees and edits it; anyone else reaches it only through
// a share, and workspace owners and admins do not see it by default. A page
// records that person: the top page of a private tree takes whoever made it,
// a sub-page takes its parent's and a database page the page holding its
// database. The pages that were outside every teamspace before, which every
// member could open, move into the workspace's default teamspace, made when
// the workspace has none, so nobody loses a page they could open.

const (
	privatePagesVersion   = 300
	privatePagesName      = "native_private_pages"
	privatePageOwnerField = "private_owner_id"
)
