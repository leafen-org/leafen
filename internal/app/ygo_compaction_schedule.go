package app

import (
	"time"

	"github.com/pocketbase/dbx"
)

// A large stream whose updates are not due yet is compacted once it has been
// left alone for ygoCompactionQuietDelay. Reading a stored document merges its
// state with every update after it, and that merge takes as long for one
// small update as for hundreds (2026-11-18: a 6.5 MB page state read in 18
// ms alone and 34 ms with 1, 64 or 339 small updates after it), so a page
// left with a few small updates on a large state paid the merge each time it
// was opened and at each read of a version restore, until edits came to an
// eighth of its state. A count of updates would leave the last few. Each
// update moves the time on, so a page being written isn't rewritten whole
// for each pause; a document smaller than ygoDocumentCacheMinBytes reads
// again at once and is left as it was.
const ygoCompactionQuietDelay = 30 * time.Second

// ygoCompactionDueSQL is whether a head's updates, counted and weighed as
// given, are due to be compacted into its state of the size given.
func ygoCompactionDueSQL(count, bytes, stateBytes string) string {
	return `(` + count + ` >= {:turnUpdates} OR ` + bytes + ` >= {:byteBudget} OR (` + count + ` >= {:batchSize}
		AND (` + bytes + `) * {:stateShare} >= ` + stateBytes + `))`
}

// ygoCompactionScheduleSQL sets a head's compaction status and next attempt
// for its updates, counted and weighed as given, on a state of the size
// given: due now (a retry's wait kept), or after the quiet delay when the
// state and its updates come to ygoDocumentCacheMinBytes; else the status is
// otherwise and the next attempt is left as it is.
func ygoCompactionScheduleSQL(count, bytes, stateBytes, otherwise string) string {
	due := ygoCompactionDueSQL(count, bytes, stateBytes)
	quiet := `(` + count + ` >= 1 AND ` + stateBytes + ` + (` + bytes + `) >= {:quietBytes})`
	return `[[compaction_status]] = CASE WHEN ` + due + ` OR ` + quiet + ` THEN 'pending' ELSE ` + otherwise + ` END,
		[[next_attempt_at]] = CASE
			WHEN ` + due + ` THEN CASE WHEN [[next_attempt_at]] > {:retryAt} THEN 0 ELSE [[next_attempt_at]] END
			WHEN ` + quiet + ` THEN {:quietAt}
			ELSE [[next_attempt_at]] END`
}

// ygoCompactionScheduleParams adds what ygoCompactionScheduleSQL binds, at
// now in Unix milliseconds, to params.
func ygoCompactionScheduleParams(params dbx.Params, now int64) dbx.Params {
	for key, value := range ygoCompactionDueParams {
		params[key] = value
	}
	params["retryAt"] = now + ygoCompactionRetryDelay.Milliseconds()
	params["quietAt"] = now + ygoCompactionQuietDelay.Milliseconds()
	params["quietBytes"] = ygoDocumentCacheMinBytes
	return params
}
