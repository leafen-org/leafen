package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A direct Notion import — one no migration runs — says while it reads about
// how many minutes reading still takes, from its pace so far
// (notionImportReadingMinutesLeft). The time it stood with Leafen off —
// stopped, restarting, the computer asleep — is none of its reading, as it is
// none of a migration's run (notion_migration_estimate_run.go), by the same
// rule: the reading is looked at every half minute while it goes, a longer
// gap since the last look is time Leafen was off (notionRunOffTime), and that
// time moves the reading's start, so its pace and the minutes left leave it
// out (2026-11-16: the move's estimate left it out while a direct import's
// minutes left still grew by the time the server was off).

// notionImportReadingClock is kept in a direct import's payload: when its
// reading was last looked at, and how long it stood with Leafen off.
type notionImportReadingClock struct {
	ReadingFollowedAt int64 `json:"readingFollowedAt,omitzero"`
	ReadingOffMs      int64 `json:"readingOffMs,omitzero"`
}

// keepNotionImportReadingClock looks at a direct import's reading: the time
// since the last look with Leafen off is added to its off time. The job's
// payload takes it at once, so a turn that Notion then fails is looked at
// too, and so does the turn's own payload, which the turn writes again.
func keepNotionImportReadingClock(app core.App, jobID string, clock *notionImportReadingClock, now time.Time) error {
	clock.ReadingOffMs += notionRunOffTime(clock.ReadingFollowedAt, now)
	clock.ReadingFollowedAt = now.UnixMilli()
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[payload_json]] = json_set([[payload_json]], '$.readingFollowedAt', {:at}, '$.readingOffMs', {:off})
		WHERE [[id]] = {:id} AND [[status]] = 'queued'
	`).Bind(dbx.Params{"at": clock.ReadingFollowedAt, "off": clock.ReadingOffMs, "id": jobID}).Execute()
	return err
}
