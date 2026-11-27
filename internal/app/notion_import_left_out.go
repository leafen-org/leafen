package app

import (
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What Leafen refuses to make of an import is left out, not the run
// (2026-11-23: a Notion move of 15,747 things stopped at 2,624 on one thing
// the content graph refused, LEAFEN_GRAPH_INVALID, and trying again stopped
// on it again at once, with nothing anywhere saying what or why). A page,
// database, row, block or file the graph refuses as it stands, after the
// fallbacks its own kind has (a block made a paragraph, a row without the
// values it refuses), takes back the window it was in — nothing of the
// window is written — and is left out: the run keeps it in its payload,
// reports it among what didn't come whole, as a thing it couldn't keep
// (block_content_invalid), logs its Notion ID, kind and why in the graph's
// own words, and makes the window again without it. What it holds — a left
// out page's blocks, a database's rows and properties — is left out with it
// and reported so. The linking after (relations, rollups, views) leaves out
// one it refuses the same way, its turn made again next. A stop for any
// other cause names the thing it was making
// (notionImportAt) and the graph's words of a refusal
// (notionImportRefusalWords), in the run's failure, its log and its status.

// notionImportLeftOutPerTurn is how many things one turn leaves out before it
// ends, the next turn going on.
const notionImportLeftOutPerTurn = 20

// notionImportItemRefusedError is a source the graph refused as it stands.
type notionImportItemRefusedError struct {
	source storedNotionSource
	err    error
}

func (e *notionImportItemRefusedError) Error() string { return e.err.Error() }
func (e *notionImportItemRefusedError) Unwrap() error { return e.err }

// notionImportItemAt names the source a window was making when it failed: one
// the graph refused, to leave out, else the page, row or block it was for.
func notionImportItemAt(source *storedNotionSource, err error) error {
	if err == nil || source == nil {
		return err
	}
	refused := &notionImportRefusedError{}
	if errors.As(err, &refused) {
		return &notionImportItemRefusedError{source: *source, err: err}
	}
	return notionImportAt(source.Kind, source.SourceID, err)
}

// notionImportLeftOut is what a window leaves out: what the run left out
// before, and what is under it, found as the window goes.
type notionImportLeftOut struct {
	ids map[string]bool
}

func newNotionImportLeftOut(payload notionImportJobPayload) *notionImportLeftOut {
	left := &notionImportLeftOut{ids: make(map[string]bool, len(payload.LeftOut))}
	for _, id := range payload.LeftOut {
		left.ids[id] = true
	}
	return left
}

// window is a window's sources less those left out: one left out, and one
// under it, reported as left out once.
func (left *notionImportLeftOut) window(app core.App, jobID string, sources []storedNotionSource) ([]storedNotionSource, error) {
	kept := make([]storedNotionSource, 0, len(sources))
	for _, source := range sources {
		switch {
		case left.ids[source.SourceID]:
		case source.ParentSourceID != "" && left.ids[source.ParentSourceID] && source.Kind == "issue":
		case source.ParentSourceID != "" && left.ids[source.ParentSourceID]:
			left.ids[source.SourceID] = true
			if err := recordNotionImportLeftOutIssue(app, jobID, source); err != nil {
				return nil, err
			}
		default:
			kept = append(kept, source)
		}
	}
	return kept, nil
}

// or is skip, and what the run left out besides: what depends on something
// left out goes without it (a row without a property left out).
func (left *notionImportLeftOut) or(skip func(string) bool) func(string) bool {
	return func(sourceID string) bool {
		return left.ids[sourceID] || skip != nil && skip(sourceID)
	}
}

// list is what is left out, for the run's payload.
func (left *notionImportLeftOut) list() []string {
	ids := make([]string, 0, len(left.ids))
	for id := range left.ids {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// leaveNotionImportItemOut leaves a refused source out of the run, its
// window not written: in the job's payload, reported and logged. It answers
// the job and payload the next window goes on from.
func (r *durableJobRunner) leaveNotionImportItemOut(
	job storedJob, payload notionImportJobPayload, refused *notionImportItemRefusedError,
) (storedJob, notionImportJobPayload, error) {
	source := refused.source
	payload.LeftOut = append(payload.LeftOut, source.SourceID)
	err := r.app.RunInTransaction(func(txApp core.App) error {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[payload_json]] = {:payload} WHERE [[id]] = {:id} AND [[status]] = 'queued'`).
			Bind(dbx.Params{"id": job.ID, "payload": string(encoded)}).Execute(); err != nil {
			return err
		}
		return recordNotionImportLeftOutIssue(txApp, job.ID, source)
	})
	if err != nil {
		return job, payload, err
	}
	attributes := append([]any{"jobId", safeDiagnosticToken(job.ID)}, notionImportDiagnosticAttributes(refused)...)
	r.app.Logger().Warn("leafen notion import left out what Leafen refused", attributes...)
	productionDiagnosticLogger.Warn("leafen notion import left out what Leafen refused", attributes...)
	current, err := loadStoredJob(r.app, job.ID, "")
	if err != nil {
		return job, payload, err
	}
	job.PayloadJSON = current.PayloadJSON
	return job, payload, nil
}

// recordNotionImportLeftOutIssue reports a source left out under the page it
// was on, as its kind: a property as one of its database's, a row or a
// database property as what the page holds (2026-11-23: a left-out title
// property, staged with no parent kind, never reached its page, and neither
// the report nor the agent's not_whole named it).
func recordNotionImportLeftOutIssue(app core.App, jobID string, source storedNotionSource) error {
	if err := recordNotionImportApplyIssue(app, jobID, source.SourceID, source.Kind, notionImportIssueInvalidBlock); err != nil {
		return err
	}
	parentKind := source.ParentKind
	switch {
	case source.Kind == "property":
		parentKind = notionImportPropertyIssueParentKind
	case parentKind == "":
		parentKind = "block"
	default:
		return nil
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_import_sources}} SET [[parent_kind]] = {:parentKind}
		WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:issue} AND [[kind]] = 'issue'
	`).Bind(dbx.Params{"jobId": jobID, "issue": source.SourceID + "#" + notionImportIssueInvalidBlock, "parentKind": parentKind}).Execute()
	return err
}

// notionImportQuoted are the quoted words a refusal's reason may hold, and
// notionImportCodeOnly a cause that is a code alone.
var (
	notionImportQuoted   = regexp.MustCompile(`"[^"]*"`)
	notionImportCodeOnly = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// notionImportRefusalWords is why the graph refused something, in its own
// words — what it checks and why — without the words it quoted of the
// person's content; "" for anything else.
func notionImportRefusalWords(err error) string {
	refusal := &graphRequestError{}
	if !errors.As(err, &refusal) {
		return ""
	}
	return strings.TrimSpace(notionImportQuoted.ReplaceAllString(refusalMessage(refusal.message, refusal.reason), `"…"`))
}

// notionImportDiagnosticAttributes are what a log line says of the thing a
// Notion import was making when it stopped or left it out: its kind and
// Notion ID, and why. A cause that names nothing a Notion move was making —
// any other job's, or the move's own outside a page, row or block — says
// nothing here: a job's line says a cause's code and class, never its words
// (2026-11-23: every job's failure line carried its cause's words as "why",
// a file cleanup's, SCIM's and Ygo compaction's SQLite and trigger words).
func notionImportDiagnosticAttributes(cause error) []any {
	var attributes []any
	refused := &notionImportItemRefusedError{}
	target := &notionImportTargetError{}
	switch {
	case errors.As(cause, &refused):
		attributes = []any{"targetKind", safeDiagnosticToken(refused.source.Kind), "notionId", safeDiagnosticToken(refused.source.SourceID)}
	case errors.As(cause, &target):
		attributes = []any{"targetKind", safeDiagnosticToken(target.kind), "notionId", safeDiagnosticToken(target.id)}
	default:
		return nil
	}
	if words := notionImportCauseWords(cause); words != "" {
		attributes = append(attributes, "why", words)
	}
	return attributes
}

// notionImportCauseWords is why a Notion import stopped or left something
// out, in words the person's content isn't quoted in: a refusal's, else the
// cause's own when it is more than a code — "" for none.
func notionImportCauseWords(cause error) string {
	if words := notionImportRefusalWords(cause); words != "" || cause == nil {
		return words
	}
	said := strings.TrimSpace(notionImportQuoted.ReplaceAllString(cause.Error(), `"…"`))
	if said == "" || notionImportCodeOnly.MatchString(said) {
		return ""
	}
	return string([]rune(said)[:min(len([]rune(said)), 300)])
}
