package app

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// How long a migration's run takes, reckoned by Leafen from its scan and said
// as a range, once, generously, with why (2026-11-16: a real move was told
// three times by three yardsticks — tens of minutes, a second a row, the
// scan's requests again — and took about two hours: the scan reads no row's
// body, and asking each block for its comments wasn't counted at all). The
// agent and the import screen say the same sentence (`say`); the run keeps
// it with the time it promised to end by, and only says it again when it
// changes, with the reason (notion_migration_estimate_run.go).
//
// A run reads every page and row again with all its blocks: the scan's own
// requests (the page bodies, databases, rows and views it listed) and, for
// each row, its body — one request, and more for each block in it that holds
// more, as the scan's sample rows showed (notion_migration_scan_sample.go),
// and again for what those hold: the sample sees only the top, and a column
// or a toggle holds blocks that hold more. Reading goes one request at a
// time, each waiting for Notion's answer and for Leafen to keep what came, so
// its pace is that wait, not Notion's three requests a second: 0.73 s a
// request on a real move (2026-11-16, notion_migration_comment_asks.go), or
// what the scan's own requests took on this server and connection when that
// was longer (counts.scanMs), and half again at the longer end for Notion
// answering slower or asking to wait (2026-11-20: a real move read its
// bodies at 3.9 s each, about five requests, where its sample's top showed
// under two, and a move of 1,081 bodies was told reading takes 10 to 20
// minutes and read for over an hour). It then makes what it read, and asks
// each page, row and block for its comments, one request each, three at once
// at Notion's pace. The longer end allows for deeper nesting than the
// sample's top, Notion answering slower, and the rest of the run.

const (
	// Notion's rate for a connection, which the comments are asked at, and a
	// slower one for the longer end (seen 2026-11-16: about 2.7 a second over
	// an hour).
	notionMigrationRequestsPerSecond     = 3.0
	notionMigrationSlowRequestsPerSecond = 2.5
	// A request read one at a time, its answer awaited and kept (2026-11-16:
	// 0.73 s on a real move), and how much longer the longer end allows.
	notionMigrationReadSecondsPerRequest = 0.73
	notionMigrationSlowReadShare         = 1.5
	// The scan's requests that time it: fewer say little of the pace.
	notionMigrationScanTimedRequests = 20
	// Another tool's bundle is read in Leafen, not over Notion's API.
	notionMigrationBundleRequestsPerSecond = 30.0
	// What Leafen makes in a second: pages, rows, blocks (2026-11-16:
	// about 13,400 in five minutes).
	notionMigrationMadePerSecond     = 60.0
	notionMigrationSlowMadePerSecond = 20.0
	// A row whose body the scan didn't sample: one request, a few blocks.
	notionMigrationUnsampledRowBlocks = 5.0
	notionMigrationUnsampledRowNested = 1.0
)

// notionMigrationEstimateParts is what an estimate is reckoned from.
type notionMigrationEstimateParts struct {
	counts                 notionMigrationCounts
	pages, databases, rows int
	comments, bundle       bool
	// A reading of the person's Notion named the blocks with comments in the
	// bodies it read whole (notion_migration_comment_discussions.go).
	discussions, discussionBodies int
	readDiscussions               bool
	// A run again, and the comments a stopped run of the same move asked
	// already (notion_migration_run_again.go), or how long the run before it
	// took, done (notion_migration_estimate_took.go).
	again bool
	asked int
	took  *notionMigrationRunTook
}

// notionMigrationEstimateOf reckons a run's time from its scan and what the
// plan moves, in the person's language.
func notionMigrationEstimateOf(parts notionMigrationEstimateParts, language string) notionMigrationEstimate {
	rows := float64(parts.rows)
	blocksPerRow, nestedPerRow, markdownPerRow := notionMigrationUnsampledRowBlocks, notionMigrationUnsampledRowNested, 0.0
	if parts.counts.SampledRows > 0 {
		blocksPerRow = float64(parts.counts.SampledRowBlocks) / float64(parts.counts.SampledRows)
		nestedPerRow = float64(parts.counts.SampledRowNested) / float64(parts.counts.SampledRows)
		markdownPerRow = float64(parts.counts.SampledRowMarkdown) / float64(parts.counts.SampledRows)
		blocksPerRow += notionMigrationInnerBlocksPerRow(parts.counts, nestedPerRow)
	}
	// The scan's requests hold the Markdown reads of pages with plain
	// headings (notionMigrationScanHeadings), which the run makes again.
	scanned := float64(parts.counts.Requests)
	// What its nested blocks hold again: as many requests more, and twice as
	// many at the longer end; and a row's Markdown, read once where its body
	// has a plain heading Notion calls childless (notion_migration_scan_sample.go).
	readFew := scanned + rows*(1+markdownPerRow+nestedPerRow*2)
	// Deeper than that, and rows in Notion's trash, which the run brings too.
	readMany := scanned*1.2 + rows*(1.2+markdownPerRow+nestedPerRow*4)
	blocksFew := float64(parts.counts.Blocks) + rows*blocksPerRow
	blocksMany := float64(parts.counts.Blocks) + rows*blocksPerRow*1.5
	fast, slow := notionMigrationRequestsPerSecond, notionMigrationSlowRequestsPerSecond
	readFast := 1 / notionMigrationReadSeconds(parts.counts)
	readSlow := readFast / notionMigrationSlowReadShare
	if parts.bundle {
		fast, slow = notionMigrationBundleRequestsPerSecond, notionMigrationBundleRequestsPerSecond/2
		readFast, readSlow = fast, slow
	}
	minutes := func(count, perSecond float64) float64 { return count / perSecond / 60 }
	items := float64(parts.pages+parts.databases) + rows
	// The sample's rows go with it: reading reckons the rows still to read
	// by them until it has read some (notion_migration_reading_work.go).
	estimate := notionMigrationEstimate{
		Pages: parts.pages, Rows: parts.rows, Blocks: int(math.Round(blocksFew)),
		SampledRows: parts.counts.SampledRows, SampledRowNested: parts.counts.SampledRowNested,
	}
	readMin, readMax := minutes(readFew, readFast), minutes(readMany, readSlow)
	makeMin, makeMax := minutes(items+blocksFew, notionMigrationMadePerSecond), minutes(items+blocksMany, notionMigrationSlowMadePerSecond)
	commentMin, commentMax := 0.0, 0.0
	if parts.comments {
		pagesAndRows := float64(parts.pages) + rows
		askFew, askMany := blocksFew, blocksMany
		// Only the blocks a reading saw comments on, where it read the body.
		if parts.readDiscussions && pagesAndRows > 0 {
			unread := max(0, 1-float64(parts.discussionBodies)/pagesAndRows)
			askFew = float64(parts.discussions) + unread*blocksFew
			askMany = float64(parts.discussions) + unread*blocksMany
		}
		// Not what a stopped run of the same move asked already: its pages
		// and rows, asked first, then its blocks.
		askedPages := min(float64(parts.asked), pagesAndRows)
		askedBlocks := float64(parts.asked) - askedPages
		askFew, askMany = max(0, askFew-askedBlocks), max(0, askMany-askedBlocks)
		estimate.CommentBlocks = int(math.Round(askFew))
		commentMin, commentMax = minutes(pagesAndRows-askedPages+askFew, fast), minutes(pagesAndRows-askedPages+askMany, slow)
	}
	// The check against Notion, the report and the rest take a few minutes;
	// a run again goes by what the run before took (notion_migration_estimate_took.go).
	reading, making, asking, rest := [2]float64{readMin, readMax}, [2]float64{makeMin, makeMax}, [2]float64{commentMin, commentMax}, [2]float64{1, 3}
	took := 0
	if parts.took != nil {
		parts.took.weigh(estimate, parts.comments, &reading, &making, &asking, &rest)
		took = int(math.Round(parts.took.whole))
	}
	low := reading[0] + making[0] + asking[0] + rest[0]
	high := (reading[1] + making[1] + asking[1] + rest[1]) * 1.2
	estimate.MinMinutes, estimate.MaxMinutes = notionMigrationRoundDown(low), notionMigrationRoundUp(high)
	estimate.ReadMinutes = notionMigrationRoundUp(reading[1])
	estimate.MakeMinutes = notionMigrationRoundUp(making[1])
	if parts.comments {
		estimate.CommentMinutes = notionMigrationRoundUp(asking[1])
	}
	read := [2]int{notionMigrationRoundDown(reading[0]), estimate.ReadMinutes}
	comments := [2]int{notionMigrationRoundDown(asking[0]), estimate.CommentMinutes}
	estimate.Say = notionMigrationEstimateSay(language, estimate, parts.bundle, read, comments)
	if parts.again {
		estimate.Say = notionMigrationRunAgainSay(language, estimate, parts.bundle, parts.asked, took, read, comments)
	}
	return estimate
}

// notionMigrationReadSeconds is how long a request read one at a time takes:
// what the scan's own requests took on this server and connection, each with
// keeping what came, or the real move's 0.73 s when that is longer or the
// scan timed too few (2026-11-20: a NAS reads more slowly than the computer
// the 0.73 s was seen on).
func notionMigrationReadSeconds(counts notionMigrationCounts) float64 {
	if counts.Requests < notionMigrationScanTimedRequests || counts.ScanMs <= 0 {
		return notionMigrationReadSecondsPerRequest
	}
	return max(notionMigrationReadSecondsPerRequest, float64(counts.ScanMs)/float64(counts.Requests)/1000)
}

// notionMigrationRoundUp rounds a time up the way a person says it: to five
// minutes within the hour, a quarter within three hours, a half beyond.
func notionMigrationRoundUp(minutes float64) int {
	step := notionMigrationRoundStep(minutes)
	return max(step, int(math.Ceil(minutes/float64(step)))*step)
}

// notionMigrationRoundNear rounds a time to the nearest the way a person says
// it, for a later end whose own margin already makes it generous (2026-11-19:
// rounded up again, 60 minutes left were said as 75).
func notionMigrationRoundNear(minutes float64) int {
	step := notionMigrationRoundStep(minutes)
	return max(step, int(math.Round(minutes/float64(step)))*step)
}

func notionMigrationRoundDown(minutes float64) int {
	step := notionMigrationRoundStep(minutes)
	return max(1, int(math.Floor(minutes/float64(step)))*step)
}

func notionMigrationRoundStep(minutes float64) int {
	switch {
	case minutes <= 60:
		return 5
	case minutes <= 180:
		return 15
	}
	return 30
}

// notionMigrationDuration says a time in minutes as a person says it.
func notionMigrationDuration(language string, minutes int) string {
	hours, rest := minutes/60, minutes%60
	if language == accountLanguageKo {
		switch {
		case hours == 0:
			return fmt.Sprintf("%d분", minutes)
		case rest == 0:
			return fmt.Sprintf("%d시간", hours)
		}
		return fmt.Sprintf("%d시간 %d분", hours, rest)
	}
	plural := func(count int, one string) string {
		if count == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %ss", count, one)
	}
	switch {
	case hours == 0:
		return plural(minutes, "minute")
	case rest == 0:
		return plural(hours, "hour")
	}
	return plural(hours, "hour") + " " + plural(rest, "minute")
}

// notionMigrationDurationRange says a range of minutes, or one time when
// both ends round alike.
func notionMigrationDurationRange(language string, low, high int) string {
	if low >= high {
		return notionMigrationDuration(language, high)
	}
	if language == accountLanguageKo {
		return notionMigrationDuration(language, low) + "~" + notionMigrationDuration(language, high)
	}
	return notionMigrationDuration(language, low) + " to " + notionMigrationDuration(language, high)
}

// notionMigrationEstimateSay is the sentence the agent and the import screen
// say: the range first, then why, what shows meanwhile and that nothing waits
// on the person.
func notionMigrationEstimateSay(language string, estimate notionMigrationEstimate, bundle bool, read, comments [2]int) string {
	whole := notionMigrationDurationRange(language, estimate.MinMinutes, estimate.MaxMinutes)
	readRange := notionMigrationDurationRange(language, read[0], read[1])
	commentRange := notionMigrationDurationRange(language, comments[0], comments[1])
	if language == accountLanguageKo {
		parts := []string{fmt.Sprintf("옮기는 데 약 %s 걸립니다.", whole)}
		bodies := fmt.Sprintf("페이지 %d개와 행 %d개", estimate.Pages, estimate.Rows)
		if estimate.Rows == 0 {
			bodies = fmt.Sprintf("페이지 %d개", estimate.Pages)
		}
		if bundle {
			parts = append(parts, bodies+"를 블록까지 하나씩 읽고 만들기 때문입니다.")
		} else {
			why := fmt.Sprintf("본문은 블록까지 Notion에 하나씩 물어 읽어야 해서, %s를 읽는 데 %s", bodies, readRange)
			if estimate.CommentBlocks > 0 {
				why += fmt.Sprintf(", 그 페이지·행과 블록 약 %d개에 댓글이 있는지 묻는 데 %s", estimate.CommentBlocks, commentRange)
			}
			parts = append(parts, why+" 걸립니다.")
		}
		parts = append(parts, "다 읽기 전에는 리펜에 아무것도 생기지 않고, 다 읽은 뒤 한꺼번에 만들어집니다. 그동안 다른 일을 하셔도 되고, 단계가 끝날 때마다 받은편지함으로 알려 드립니다.")
		return strings.Join(parts, " ")
	}
	parts := []string{fmt.Sprintf("The move takes about %s.", whole)}
	bodies := fmt.Sprintf("%d pages and %d rows", estimate.Pages, estimate.Rows)
	if estimate.Rows == 0 {
		bodies = fmt.Sprintf("%d pages", estimate.Pages)
	}
	if bundle {
		parts = append(parts, "Leafen reads and makes "+bodies+" block by block.")
	} else {
		why := "Notion gives a body block by block, one request at a time, so reading " + bodies + " takes " + readRange
		if estimate.CommentBlocks > 0 {
			why += fmt.Sprintf(", and asking them and about %d blocks for their comments %s", estimate.CommentBlocks, commentRange)
		}
		parts = append(parts, why+".")
	}
	parts = append(parts, "Nothing shows in Leafen until reading ends; then it is made all at once. You can do other things meanwhile: Leafen tells you in your inbox as each stage ends.")
	return strings.Join(parts, " ")
}

// notionMigrationPlanEstimate is how long running a migration's plan takes,
// as its plan's answer and the import screen say it before the run.
func notionMigrationPlanEstimate(ctx context.Context, app core.App, stored storedNotionMigration, summary notionMigrationSummary, state notionMigrationPlanState) *notionMigrationEstimate {
	plan := state.Plan
	if plan == nil {
		plan = state.Draft
	}
	if plan == nil {
		return nil
	}
	parts := notionMigrationEstimateParts{
		counts: summary.Counts, pages: summary.Counts.Pages, databases: len(summary.Databases),
		comments: plan.Comments == "import", bundle: stored.SourceLabel != "",
	}
	for _, database := range summary.Databases {
		parts.rows += database.Rows
	}
	parts.discussions, parts.discussionBodies, parts.readDiscussions = notionMigrationReadDiscussions(app, stored.ID)
	parts.again, parts.asked = notionMigrationEstimateAgain(app, stored)
	if parts.again {
		if took, err := notionMigrationLastRunTook(stored); err == nil {
			parts.took = took
		}
	}
	if state.Check != nil {
		parts.pages, parts.databases, parts.rows = state.Check.Expected.Pages, state.Check.Expected.Databases, state.Check.Expected.Rows
	}
	language, err := serverTextLanguageIn(ctx, app, stored.Owner)
	if err != nil {
		language = accountLanguageEn
	}
	estimate := notionMigrationEstimateOf(parts, language)
	return &estimate
}

// startNotionMigrationEstimate keeps, as a run starts, the time it is
// promised to end by: the estimate's longer end.
func startNotionMigrationEstimate(app core.App, stored storedNotionMigration, startedAt int64) error {
	state, err := notionMigrationPlanStateOf(context.Background(), app, stored)
	if err != nil || state.Estimate == nil {
		return err
	}
	estimate := *state.Estimate
	estimate.StartedAt = startedAt
	estimate.EndsAt = startedAt + int64(estimate.MaxMinutes)*time.Minute.Milliseconds()
	estimate.Stage, estimate.StageAt = notionMigrationStageReading, startedAt
	return updateNotionMigrationRunReport(app, stored.ID, func(report *notionMigrationRunReport) {
		report.Estimate, report.CommentsStopped = &estimate, false
	})
}

// notionMigrationInnerBlocksPerRow is how many blocks a row's blocks that
// hold more hold, as the scan's sample listed a few of them
// (notion_migration_scan_sample.go): each holds what those held, and those
// that hold more again as many again, as reading reckons the levels below
// (notion_migration_reading_work.go). A run makes them and asks each for its
// comments as it does the top (2026-11-20: reckoned from the top alone, a
// move said 402 blocks to ask and asked 761). 0 where the scan listed none.
func notionMigrationInnerBlocksPerRow(counts notionMigrationCounts, nestedPerRow float64) float64 {
	if counts.SampledInnerLists <= 0 {
		return 0
	}
	held := float64(counts.SampledInnerBlocks) / float64(counts.SampledInnerLists)
	again := float64(counts.SampledInnerNested) / float64(counts.SampledInnerLists)
	return nestedPerRow * held * (1 + again)
}
