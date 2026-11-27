package app

// A template's repeat may end after so many pages ("실행 횟수", as Notion's
// does, seen 2026-10-30), as a recurring automation's does: its state counts
// the pages it was admitted for, through any edit of the repeat, and once
// the count is reached it makes no more.

// databaseTemplateRepeatRunsLeft says whether a repeat that ends after so
// many pages has pages left after those it was admitted for.
func databaseTemplateRepeatRunsLeft(trigger databaseAutomationTrigger, runs int) bool {
	return trigger.EndsAfter <= 0 || runs < trigger.EndsAfter
}
