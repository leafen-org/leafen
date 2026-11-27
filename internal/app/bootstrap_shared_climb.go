package app

// Whether the walk of bootstrapSharedWalkSQL reaches a page is read here
// without walking down every page shared with the person: from the page up
// through those the walk would reach each from the one above it
// (bootstrapSharedStepSQL), as far as one it starts from
// (bootstrapSharedBaseSQL) — the walk read the other way, which costs the
// page's depth instead of every shared page's.

// bootstrapSharedBasesSQL names, after bootstrapShareRootsSQL's names, as
// shared_base, where the walk starts; a statement goes on with more of its
// names.
var bootstrapSharedBasesSQL = `
		WITH RECURSIVE` + bootstrapShareRootsSQL + `,
		[[shared_base]] ([[id]], [[workspace]]) AS (` + bootstrapSharedBaseSQL + `)`

// bootstrapSharedClimbSQL names, after bootstrapSharedBasesSQL, as
// climb_start the pages to climb from — start, a SELECT of (slot, page ID)
// rows — and as shared_up the pages above each, by its slot, that the walk
// passes on the way up: each page above one the walk would reach from it,
// until one the walk starts from.
func bootstrapSharedClimbSQL(start string) string {
	return `,
		[[climb_start]] ([[slot]], [[id]]) AS (` + start + `)` +
		bootstrapSharedClimbOneSQL("shared_up", "shared_base", bootstrapSharedStepSQL("[[up_node]]"))
}

// bootstrapSharedClimbOneSQL names the walk's climb (bootstrapSharedClimbSQL).
func bootstrapSharedClimbOneSQL(name, base, step string) string {
	return `,
		[[` + name + `]] ([[slot]], [[id]], [[workspace]]) AS (
			SELECT [[climb_start]].[[slot]], [[start_page]].[[id]], [[start_page]].[[workspace]]
			FROM [[climb_start]] JOIN {{leafen_pages}} [[start_page]] ON [[start_page]].[[id]] = [[climb_start]].[[id]]
			UNION
			SELECT [[` + name + `]].[[slot]], [[up_parent]].[[id]], [[up_parent]].[[workspace]]
			FROM [[` + name + `]]
			JOIN {{leafen_pages}} [[up_node]] ON [[up_node]].[[id]] = [[` + name + `]].[[id]]
			JOIN {{leafen_pages}} [[up_parent]] ON [[up_parent]].[[id]] = [[up_node]].[[parent_id]]
				AND [[up_parent]].[[workspace]] = [[up_node]].[[workspace]]
			WHERE ([[` + name + `]].[[id]], [[` + name + `]].[[workspace]]) NOT IN (SELECT [[id]], [[workspace]] FROM [[` + base + `]])
				AND ` + step + `
		)`
}

// bootstrapSharedReachedSQL says, after bootstrapSharedClimbSQL, that the
// walk climbed as climb, from base, reaches the start page of the slot
// given by an SQL expression: its climb meets a page the walk starts from.
func bootstrapSharedReachedSQL(climb, base, slot string) string {
	return `EXISTS (SELECT 1 FROM [[` + climb + `]] WHERE [[` + climb + `]].[[slot]] = ` + slot + `
				AND ([[` + climb + `]].[[id]], [[` + climb + `]].[[workspace]]) IN (SELECT [[id]], [[workspace]] FROM [[` + base + `]]))`
}
