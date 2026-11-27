package app

import (
	"encoding/json"
	"time"
)

// A migration's status is short unless the agent asks for detail
// (2026-11-22: a status answer of about 9KB, most of it the estimate's own
// reckoning and the import's counters, read again and again while a run
// goes): the migration and its import keep what an agent acts on and tells
// the person, under the names the next step speaks of (runReport.estimate,
// runReport.verify, runReport.tally), without what is empty, nought or false;
// while the run goes, what hasn't come whole comes by its counts alone; and
// detail true gives all of it as stored.

// What a short status keeps of each, by its JSON name.
var (
	notionMigrationShortKeys = []string{"id", "workspaceId", "roots", "status", "counts", "errorCode", "runState",
		"runPlanRevision", "importJobId", "importTopPageId", "runReport", "runEndedAt"}
	notionMigrationCountsShortKeys   = []string{"pages", "databases", "rows", "blocks"}
	notionMigrationEstimateShortKeys = []string{"minMinutes", "maxMinutes", "say", "reason", "startedAt", "endsAt", "changedAt", "stage"}
	notionImportJobShortKeys         = []string{"id", "status", "stage", "step", "errorCode", "commentBlocksDone", "commentBlocksTotal",
		"commentPagesDone", "commentPagesTotal", "report", "progress"}
	// The import's report keeps its counts; what didn't come whole is said in not_whole.
	notionImportReportShortKeys = []string{"pages", "blocks", "databases", "rows", "filesCopied", "filesOmitted",
		"blockIssues", "propertyIssues", "viewIssues"}
	// Its progress keeps what Leafen's import window shows: the share done, in
	// whole percent (percentDone; timeShare counts thousandths for the
	// window's bar, and an agent told a person 63 of them as "63%",
	// 2026-11-23), and the time left in minutes.
	notionImportProgressShortKeys = []string{"percentDone", "minutesLeft", "changedAt", "tally"}
	// The noughts a short answer keeps: the share done and the time left say so at nought too.
	notionStatusZeroSays = map[string]bool{"percentDone": true, "minutesLeft": true}
)

// notionMigrationStatusFields is a migration as a status answer carries it,
// its words in English (notionMigrationStatusFieldsIn).
func notionMigrationStatusFields(migration notionMigration, detail bool) map[string]any {
	return notionMigrationStatusFieldsIn(migration, detail, accountLanguageEn)
}

// notionMigrationStatusFieldsIn is a migration as a status answer carries it:
// whole with detail, else short — a running one's estimate, once its run
// keeps one, by its time left (notionMigrationEstimateLeftFields), in the
// person's language.
func notionMigrationStatusFieldsIn(migration notionMigration, detail bool, language string) map[string]any {
	fields := jsonFieldsOf(migration)
	if detail || fields == nil {
		return fields
	}
	fields = keepFields(fields, notionMigrationShortKeys)
	if counts, ok := fields["counts"].(map[string]any); ok {
		fields["counts"] = keepFields(counts, notionMigrationCountsShortKeys)
	}
	if report, ok := fields["runReport"].(map[string]any); ok {
		if estimate, ok := report["estimate"].(map[string]any); ok {
			kept := keepFields(estimate, notionMigrationEstimateShortKeys)
			if migration.RunState == notionMigrationRunRunning && migration.RunReport.Estimate != nil && migration.RunReport.Estimate.EndsAt > 0 {
				notionMigrationEstimateLeftFields(kept, *migration.RunReport.Estimate, language, time.Now())
			}
			report["estimate"] = kept
		}
	}
	return withoutEmpty(fields)
}

// notionImportStatusFields is a migration's import as a status answer
// carries it: whole with detail, else short; nil with none.
func notionImportStatusFields(job *notionImportJob, detail bool) map[string]any {
	if job == nil {
		return nil
	}
	fields := jsonFieldsOf(job)
	if detail || fields == nil {
		return fields
	}
	fields = keepFields(fields, notionImportJobShortKeys)
	if report, ok := fields["report"].(map[string]any); ok {
		fields["report"] = keepFields(report, notionImportReportShortKeys)
	}
	if progress, ok := fields["progress"].(map[string]any); ok {
		fields["progress"] = keepFields(progress, notionImportProgressShortKeys)
	}
	return withoutEmpty(fields)
}

// notionImportGoingFields is a running import as a short status carries it:
// without its counts — what it read, made and asked for comments so far —
// which Leafen's import window shows live and the person follows there
// (user decision 2026-11-23 "묶어서 올려"; the same day an agent passed them
// on at each status). Its stage, step, progress (the share done and the time
// left) and why it stopped stay; detail gives the counts.
func notionImportGoingFields(fields map[string]any) map[string]any {
	for _, key := range []string{"commentBlocksDone", "commentBlocksTotal", "commentPagesDone", "commentPagesTotal", "report"} {
		delete(fields, key)
	}
	return fields
}

// withoutEmpty is the fields without those empty, nought or false, all the
// way down; notionStatusZeroSays keep their noughts.
func withoutEmpty(fields map[string]any) map[string]any {
	for key, value := range fields {
		switch typed := value.(type) {
		case map[string]any:
			if kept := withoutEmpty(typed); len(kept) > 0 {
				fields[key] = kept
				continue
			}
		case []any:
			for index, item := range typed {
				if inner, ok := item.(map[string]any); ok {
					typed[index] = withoutEmpty(inner)
				}
			}
			if len(typed) > 0 {
				continue
			}
		case string:
			if typed != "" {
				continue
			}
		case bool:
			if typed {
				continue
			}
		case float64:
			if typed != 0 || notionStatusZeroSays[key] {
				continue
			}
		}
		delete(fields, key)
	}
	return fields
}

// jsonFieldsOf is a value's JSON object as fields; nil if it isn't one.
func jsonFieldsOf(value any) map[string]any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	fields := map[string]any{}
	if json.Unmarshal(encoded, &fields) != nil {
		return nil
	}
	return fields
}

// keepFields is the fields named, those there are.
func keepFields(fields map[string]any, keys []string) map[string]any {
	kept := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, ok := fields[key]; ok {
			kept[key] = value
		}
	}
	return kept
}
