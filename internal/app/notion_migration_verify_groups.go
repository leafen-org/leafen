package app

// A check lists at most notionMigrationVerifyShown of the differences it
// finds, and says every one of them by its group (2026-11-21: a check of more
// than 200 counted its groups from the 200 it kept, in the status and on the
// report page, so the groups added up to less than remainingTotal — and when
// all it kept had a reason, its next said nothing was left to fix while
// differences to fix lay past the 200). So it counts each group — what
// differs and why — over all it found, and keeps those its next asks the
// agent to fix (notionMigrationVerifyFixable) before the rest, each in the
// order it found them; the report page and the answers say the groups from
// these counts (notionMigrationVerifyGroups) and list only the kept.

// notionMigrationVerifyGroupKey is the group a difference is said in: what
// differs and why.
func notionMigrationVerifyGroupKey(kind, setting, reason string) string {
	return kind + "\x00" + setting + "\x00" + reason
}

// notionMigrationVerifyGroupCounts counts all a check found by group, the
// groups in the order their first difference came.
func notionMigrationVerifyGroupCounts(found []notionMigrationVerifyDifference) []notionMigrationVerifyGroupCount {
	counts, at := []notionMigrationVerifyGroupCount{}, map[string]int{}
	for _, difference := range found {
		key := notionMigrationVerifyGroupKey(difference.Kind, difference.Setting, difference.Reason)
		index, seen := at[key]
		if !seen {
			index, at[key] = len(counts), len(counts)
			counts = append(counts, notionMigrationVerifyGroupCount{Kind: difference.Kind, Setting: difference.Setting, Reason: difference.Reason})
		}
		counts[index].Count++
	}
	return counts
}

// notionMigrationVerifyKept are the differences a check lists: all of them
// when they fit, else those to fix first and then the others, as many as fit,
// in the order the check found them.
func notionMigrationVerifyKept(found []notionMigrationVerifyDifference) []notionMigrationVerifyDifference {
	if len(found) <= notionMigrationVerifyShown {
		return append([]notionMigrationVerifyDifference{}, found...)
	}
	keep, room := make([]bool, len(found)), notionMigrationVerifyShown
	for _, fixable := range []bool{true, false} {
		for index, difference := range found {
			if room > 0 && notionMigrationVerifyFixable(difference) == fixable {
				keep[index], room = true, room-1
			}
		}
	}
	kept := make([]notionMigrationVerifyDifference, 0, notionMigrationVerifyShown)
	for index, difference := range found {
		if keep[index] {
			kept = append(kept, difference)
		}
	}
	return kept
}
