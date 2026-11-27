package app

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The request log (PocketBase's _logs in auxiliary.db) keeps what tells an
// administrator what went wrong (2026-10-18: a 400-person load wrote 125 MB
// an hour, 15 GB for the 5 days kept): every failed request, every slow one,
// every change an administrator, a sign-in or the setup makes, and of the
// other successful requests one in a hundred — or all of them, or none, as
// the administrator chooses. It keeps them for the days the administrator
// sets, 5 unless changed, and at most 300,000 entries, the oldest going
// first. The organization audit, the server administrator log and the read
// log are kept apart and stay as they are.

const (
	requestLogSlow        = time.Second
	requestLogSampleEvery = 100
	// About 300 MB at the size entries take.
	requestLogMaxEntries = 300_000
	requestLogTrimTo     = 270_000
	// PocketBase's requestEventKeySkipSuccessActivityLog (apis/middlewares.go):
	// set on a request that succeeded, its activity logger keeps no entry.
	pocketBaseSkipSuccessLogKey = "__skipSuccessActivityLogger"
)

// requestLogSuccessful is the administrator's choice for successful
// requests, as last read; requestLogSeen counts those the sample is taken
// from.
var (
	requestLogSuccessful atomic.Value
	requestLogSeen       atomic.Uint64
)

func requestLogMode() string {
	if mode, ok := requestLogSuccessful.Load().(string); ok {
		return mode
	}
	return requestLogSuccessfulSample
}

// keepRequestLog tells PocketBase's activity logger, which wraps every
// request, to keep no entry for a successful request the policy leaves out.
// A failed request — an error, or a 4xx or 5xx answer — is always kept; a
// streamed one is judged by how it started (request_log_streams.go).
func keepRequestLog(event *core.RequestEvent) error {
	started := time.Now()
	var stream *requestLogStream
	if requestLogStreamed(event.Request) {
		stream = &requestLogStream{ResponseWriter: event.Response, started: started}
		event.Response = stream
	}
	err := event.Next()
	if stream != nil {
		event.Response = stream.ResponseWriter
	}
	if err != nil {
		return err
	}
	status, took := event.Status(), time.Since(started)
	if stream != nil {
		status, took = stream.outcome(status)
	}
	if status == 0 || status >= http.StatusBadRequest || took >= requestLogSlow || requestLogAlwaysKept(event.Request) {
		return nil
	}
	switch requestLogMode() {
	case requestLogSuccessfulAll:
		return nil
	case requestLogSuccessfulSample:
		if requestLogSeen.Add(1)%requestLogSampleEvery == 0 {
			return nil
		}
	}
	event.Set(pocketBaseSkipSuccessLogKey, true)
	return nil
}

// requestLogAlwaysKept is a change an administrator, a sign-in or the setup
// makes.
func requestLogAlwaysKept(request *http.Request) bool {
	switch request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	path := request.URL.Path
	for _, prefix := range []string{apiRoot + "/admin/", apiRoot + "/setup", apiRoot + "/auth/", apiRoot + "/organizations/", "/api/collections/_superusers/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func loadRequestLogMode(app core.App) (string, int, error) {
	var stored struct {
		Successful string `db:"successful"`
		Revision   int    `db:"revision"`
	}
	err := app.DB().NewQuery(`SELECT [[successful]], [[revision]] FROM {{leafen_request_log_settings}} WHERE [[id]] = 'default'`).One(&stored)
	return stored.Successful, stored.Revision, err
}

// trimRequestLog lets go of the oldest entries past the most kept.
func trimRequestLog(app core.App) error {
	var entries int64
	if err := app.AuxDB().NewQuery(`SELECT COUNT(*) FROM {{_logs}}`).Row(&entries); err != nil {
		return err
	}
	if entries <= requestLogMaxEntries {
		return nil
	}
	_, err := app.AuxNonconcurrentDB().NewQuery(`
		DELETE FROM {{_logs}} WHERE [[id]] IN (SELECT [[id]] FROM {{_logs}} ORDER BY [[created]] ASC LIMIT {:count})
	`).Bind(dbx.Params{"count": entries - requestLogTrimTo}).Execute()
	return err
}

func projectRequestLog(app core.App, mode string) administratorRequestLog {
	var entries int64
	_ = app.AuxDB().NewQuery(`SELECT COUNT(*) FROM {{_logs}}`).Row(&entries)
	return administratorRequestLog{RetentionDays: app.Settings().Logs.MaxDays, Successful: mode, Entries: entries,
		MaxEntries: requestLogMaxEntries}
}

func readRequestLogPolicy(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	mode, _, err := loadRequestLogMode(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the request log settings.", err)
	}
	return event.JSON(http.StatusOK, projectRequestLog(event.App, mode))
}

// saveRequestLogPolicy sets how long the request log keeps entries and
// which successful requests it keeps, kept in the server audit log.
func saveRequestLogPolicy(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	input := administratorRequestLogInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the request log settings.")
	}
	if input.RetentionDays == 0 {
		return refuseMissingInput(event, errorCodeInvalidInput, "retentionDays", "how many days the request log keeps its lines, 1 to 90.")
	}
	if input.Successful == "" {
		return refuseMissingInput(event, errorCodeInvalidInput, "successful", "which successful requests the log keeps: sample, all or none.")
	}
	if input.RetentionDays < minRequestLogRetentionDays ||
		input.RetentionDays > maxRequestLogRetentionDays ||
		(input.Successful != requestLogSuccessfulSample && input.Successful != requestLogSuccessfulAll && input.Successful != requestLogSuccessfulNone) {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the request log settings.")
	}
	mode, revision, err := loadRequestLogMode(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the request log settings.", err)
	}
	actor := ""
	if event.Auth != nil {
		actor = event.Auth.Id
	}
	retention := event.App.Settings().Logs.MaxDays
	err = event.App.RunInTransaction(func(tx core.App) error {
		if input.Successful == mode && input.RetentionDays == retention {
			return nil
		}
		revision++
		if _, err := tx.DB().NewQuery(`UPDATE {{leafen_request_log_settings}} SET [[successful]] = {:mode}, [[revision]] = {:revision}
			WHERE [[id]] = 'default'`).Bind(dbx.Params{"mode": input.Successful, "revision": revision}).Execute(); err != nil {
			return err
		}
		// The server audit's revisions are the main settings'; these
		// count apart, under their own names.
		at := time.Now().UnixMilli()
		for _, change := range [][3]string{
			{"request_log_retention_days", strconv.Itoa(retention), strconv.Itoa(input.RetentionDays)},
			{"request_log_successful", mode, input.Successful},
		} {
			if change[1] == change[2] {
				continue
			}
			if _, err := tx.DB().Insert("leafen_server_settings_audit_events", dbx.Params{
				"id": core.GenerateDefaultRandomId(), "actor_id": actor, "revision": revision, "field": change[0],
				"from_value": change[1], "to_value": change[2], "created_at": at,
			}).Execute(); err != nil {
				return err
			}
		}
		if input.RetentionDays != retention {
			settings, err := tx.Settings().Clone()
			if err != nil {
				return err
			}
			settings.Logs.MaxDays = input.RetentionDays
			return tx.Save(settings)
		}
		return nil
	})
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the request log settings.", err)
	}
	requestLogSuccessful.Store(input.Successful)
	return event.JSON(http.StatusOK, projectRequestLog(event.App, input.Successful))
}

// registerRequestLogPolicy applies the policy to every request, keeps the
// log within its most entries at the start and every half hour, and serves
// its settings.
func registerRequestLogPolicy(event *core.ServeEvent) {
	if mode, _, err := loadRequestLogMode(event.App); err == nil {
		requestLogSuccessful.Store(mode)
	}
	event.Router.BindFunc(keepRequestLog)
	// Behind the start and the clock, stopped with the server (server_background.go).
	trim := func() {
		serverBackgroundOf(event.App).run("request log trim", func(context.Context) {
			if err := trimRequestLog(event.App); err != nil {
				event.App.Logger().Warn("request log: couldn't let go of the oldest entries", "error", err)
			}
		})
	}
	// At the start too: a server that was stopped catches up at once.
	trim()
	event.App.Cron().MustAdd("leafenRequestLogTrim", "*/30 * * * *", trim)
	event.Router.GET(apiRoot+"/admin/request-log", readRequestLogPolicy)
	event.Router.PUT(apiRoot+"/admin/request-log", saveRequestLogPolicy)
}
