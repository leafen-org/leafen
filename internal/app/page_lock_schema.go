package app

// pageLockedBy is who a page's lock names after a change: the one locking it
// now, whoever locked it before while it stays locked, or no one unlocked.
func pageLockedBy(wasLocked, locked bool, before, actorID string) string {
	switch {
	case !locked:
		return ""
	case wasLocked:
		return before
	default:
		return actorID
	}
}
