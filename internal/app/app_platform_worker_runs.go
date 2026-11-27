//go:build !leafen_noapps

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Running an app's worker functions (docs/app-platform.md 10·11): each run
// is recorded with its trigger and outcome; the worker reaches Leafen only
// through host functions the server answers — its log, the organizations
// it serves, the native API as the app's account in its own workspace, and
// the relay. An incoming hook's call runs its worker; one that fails is
// tried again later, five times in all.

const (
	appRunMaxHostCalls  = 200
	appRunMaxLogBytes   = 16 << 10
	appHookMaxAttempts  = 5
	appHookRetryBase    = 30 * time.Second
	appExecutorInterval = 3 * time.Second
)

// appRunTimeout bounds one run of a worker that declares no time of its
// own; checks shorten it.
var appRunTimeout = 10 * time.Second

// appWorkerRunLimits is one run's time and memory: what its worker declares
// (up to a minute and 256 MB), the platform's otherwise; a phone keeps to
// its smaller memory.
func appWorkerRunLimits(worker appManifestWorker) (time.Duration, uint32) {
	timeout, pages := appRunTimeout, appWorkerLimits.memoryPages
	if worker.TimeoutSeconds > 0 {
		timeout = time.Duration(min(worker.TimeoutSeconds, 60)) * time.Second
	}
	if worker.MemoryMB > 0 {
		pages = min(uint32(min(worker.MemoryMB, 256))*16, appWorkerLimits.maxMemoryPages)
	}
	return timeout, pages
}

type appWorkerHost struct {
	ctx      context.Context
	app      core.App
	install  storedAppInstall
	manifest appManifest
	run      appRun
	logs     strings.Builder
	calls    int
	// The run's open sockets (app_platform_sockets.go).
	sockets    map[int]*appSocket
	nextSocket int
	// A long-running worker's run, counted by the minute instead
	// (app_platform_long_runs.go).
	long        *appLongRun
	minute      int64
	minuteCalls int
	// A room loop's room (app_platform_room_loops.go).
	roomLoop *appRoomLoop
	// The person's credentials the run fills (app_platform_credentials.go).
	credentials *appCredentialFiller
	// What the run changed besides page history (app_platform_run_effects.go).
	effects appRunEffects
}

// credentialFiller fills the credentials of the person the run is for — an
// action run as them or a per-person schedule — and of no one otherwise.
func (host *appWorkerHost) credentialFiller() *appCredentialFiller {
	if host.credentials == nil {
		person := ""
		if host.run.Person != nil {
			person = host.run.Person.OwnerID
		}
		host.credentials = newAppCredentialFiller(host.app, host.install, host.manifest, person)
	}
	return host.credentials
}

// appRun is one run of a worker function: what triggered it, for whom, and
// whose authority its API calls carry — the app's account in the run's
// organization, or the person's own app token for an action run as them.
type appRun struct {
	Trigger, Ref, Subject string
	Worker, Function      string
	Input                 []byte
	Tenancy               string
	Person                *userKeyAuthority
}

// appRunBoundError is a run's bound passed: it ends the run, as its time and
// memory do, rather than reaching the module as a refusal.
type appRunBoundError string

func (bound appRunBoundError) Error() string { return string(bound) }

func (host *appWorkerHost) answer(name string, payload []byte) ([]byte, error) {
	host.calls++
	if host.long != nil {
		if minute := time.Now().Unix() / 60; minute != host.minute {
			host.minute, host.minuteCalls = minute, 0
		}
		host.minuteCalls++
		if host.minuteCalls > appLongRunCallsPerMinute {
			return nil, appRunBoundError(fmt.Sprintf("more than %d host calls in a minute", appLongRunCallsPerMinute))
		}
	} else if host.calls > appRunMaxHostCalls {
		return nil, appRunBoundError(fmt.Sprintf("more than %d host calls in one run", appRunMaxHostCalls))
	}
	switch name {
	case "leafen_socket_open", "leafen_socket_write", "leafen_socket_read", "leafen_socket_close":
		return host.socket(name, payload)
	case "leafen_wait":
		return host.wait(payload)
	case "leafen_room_next":
		return host.roomNext(payload)
	case "leafen_room_send":
		return host.roomSend(payload)
	case "leafen_log":
		if room := appRunMaxLogBytes - host.logs.Len(); room > 0 {
			line := string(payload)
			if len(line) > room {
				line = line[:room]
			}
			host.logs.WriteString(line)
			host.logs.WriteByte('\n')
		}
		return []byte("{}"), nil
	case "leafen_tenancies":
		workspaces, err := loadAppWorkspaces(host.app, host.install.ID)
		if err != nil {
			return nil, err
		}
		listed := []map[string]string{}
		for _, workspace := range workspaces {
			listed = append(listed, map[string]string{"tenancy": workspace.TenancyKey, "workspaceId": workspace.WorkspaceID})
		}
		return json.Marshal(listed)
	case "leafen_api":
		return host.api(payload)
	case "leafen_kv":
		request := appKVRequest{}
		if err := json.Unmarshal(payload, &request); err != nil {
			return nil, errors.New("leafen_kv takes a store command")
		}
		tenancy := host.run.Tenancy
		if tenancy == "" && appWorkspaceTenancyKnown(host.app, host.install.ID, request.Tenancy) {
			tenancy = request.Tenancy
		}
		if tenancy == "" {
			return nil, errors.New("leafen_kv needs the organization when the run has none")
		}
		result, err := runAppKV(host.manifest, host.install.ID, tenancy, "app:"+host.run.Subject, request, time.Now())
		if err != nil {
			return json.Marshal(map[string]string{"error": err.Error()})
		}
		return json.Marshal(map[string]any{"result": result})
	case "leafen_notify":
		request := appNoticeRequest{}
		if err := json.Unmarshal(payload, &request); err != nil {
			return nil, errors.New("leafen_notify takes a person, a text and a path")
		}
		tenancy := host.run.Tenancy
		if tenancy == "" && appWorkspaceTenancyKnown(host.app, host.install.ID, request.Tenancy) {
			tenancy = request.Tenancy
		}
		if err := sendAppNotice(host.app, host.install, host.manifest, tenancy, request, time.Now()); err != nil {
			return json.Marshal(map[string]string{"error": err.Error()})
		}
		return []byte(`{"ok":true}`), nil
	case "leafen_files_join":
		return host.joinFiles(payload)
	case "leafen_relay":
		input := appRelayInput{}
		if err := json.Unmarshal(payload, &input); err != nil {
			return nil, errors.New("leafen_relay takes a relay request")
		}
		settings, _ := loadAppPlatformSettings(host.app)
		answer, err := runAppRelay(host.ctx, appRelayCall{App: host.app, Install: host.install, Manifest: host.manifest,
			PersonID: host.run.Subject, Tenancy: host.run.Tenancy, Strict: settings.StrictNetwork || appTenancyStrict(host.app, host.run.Tenancy),
			Credentials: host.credentialFiller()}, input)
		if err != nil {
			return json.Marshal(map[string]string{"error": err.Error()})
		}
		return json.Marshal(answer)
	}
	return nil, fmt.Errorf("no host function %s", name)
}

// api calls the native API as the app's account, in its workspace of the
// organization the worker names.
func (host *appWorkerHost) api(payload []byte) ([]byte, error) {
	request := struct {
		Tenancy string          `json:"tenancy"`
		Method  string          `json:"method"`
		Path    string          `json:"path"`
		Query   string          `json:"query"`
		Body    json.RawMessage `json:"body"`
	}{}
	if err := json.Unmarshal(payload, &request); err != nil || !strings.HasPrefix(request.Path, "/") || strings.Contains(request.Path, "..") {
		return nil, errors.New("leafen_api takes a tenancy, a method and a path")
	}
	call := func(authority userKeyAuthority) ([]byte, error) {
		result, err := callNativeOperation(host.ctx, authority, request.Method, request.Path, request.Query, request.Body, "")
		if err != nil {
			return nil, err
		}
		noteAppRunRefusal(host.ctx, result)
		host.effects.note(request.Method, request.Path, result)
		return json.Marshal(nativeOperationAppAnswer(result))
	}
	// An action run as the person reaches what their app token reaches.
	if host.run.Person != nil {
		return call(*host.run.Person)
	}
	if request.Tenancy == "" {
		request.Tenancy = host.run.Tenancy
	}
	workspaces, err := loadAppWorkspaces(host.app, host.install.ID)
	if err != nil {
		return nil, err
	}
	for _, workspace := range workspaces {
		if workspace.TenancyKey != request.Tenancy {
			continue
		}
		connectionID, err := appAccountConnection(host.app, host.install, host.manifest, workspace)
		if err != nil {
			return nil, err
		}
		return call(userKeyAuthority{
			ConnectionID: connectionID, OwnerID: workspace.AccountID, Scope: userKeyScopeStructure,
			WorkspaceIDs: []string{workspace.WorkspaceID}, Created: time.Now().UnixMilli(), AppInstallID: host.install.ID,
			TenancyKey: workspace.TenancyKey, AppAccount: true,
		})
	}
	return nil, fmt.Errorf("the app has no workspace for %s", request.Tenancy)
}

// runAppWorker runs a function and records the run; a long-running
// worker's run (long) has a worker of its own for up to an hour.
func runAppWorker(ctx context.Context, app core.App, install storedAppInstall, manifest appManifest, run appRun, long *appLongRun) ([]byte, string, error) {
	return runAppWorkerWith(ctx, app, install, manifest, run, long, nil)
}

// runAppWorkerWith runs it as a room's loop too when roomLoop is given.
func runAppWorkerWith(ctx context.Context, app core.App, install storedAppInstall, manifest appManifest, run appRun, long *appLongRun,
	roomLoop *appRoomLoop) ([]byte, string, error) {
	module, resident := "", ""
	timeout, pages := appRunTimeout, appWorkerLimits.memoryPages
	for _, worker := range manifest.Workers {
		if worker.Name == run.Worker {
			module = worker.Module
			timeout, pages = appWorkerRunLimits(worker)
			// An endpoint's calls keep a resident worker's instance.
			if worker.Resident && run.Trigger == "endpoint" {
				resident = install.ID + "/" + worker.Name
			}
		}
	}
	wasm, err := loadAppPackageFile(app, install.PackageHash, module)
	if module == "" || err != nil {
		return nil, "", fmt.Errorf("the app has no worker %s", run.Worker)
	}
	digest := sha256.Sum256(wasm)
	inputDigest := sha256.Sum256(run.Input)
	runID := security.RandomString(20)
	started := time.Now()
	if _, err := app.DB().Insert(appRunsTable, dbx.Params{
		"id": runID, "install_id": install.ID, "trigger": run.Trigger, "trigger_ref": run.Ref, "subject": run.Subject, "worker": run.Worker,
		"function": run.Function, "input_hash": hex.EncodeToString(inputDigest[:]), "status": "running", "created": started.UnixMilli(),
		"tenancy": run.Tenancy, "connection_id": appRunConnection(app, install, run), "undo_input": appRunUndoInput(manifest, run),
		"place": appRunPlace(run), "pages_recorded": 1,
	}).Execute(); err != nil {
		return nil, "", err
	}
	// Sockets that name an address the administrator sets open there.
	if values, err := loadAppSettingValues(app, install.ID); err == nil {
		manifest = appManifestWithSettings(manifest, values)
	}
	host := &appWorkerHost{ctx: withAppRunID(ctx, runID), app: app, install: install, manifest: manifest, run: run, long: long, roomLoop: roomLoop}
	// A module is kept loaded for the memory it was loaded with.
	call := appWorkerCall{
		InstallID: install.ID, Module: fmt.Sprintf("%x/%d", digest, pages), Wasm: wasm, Function: run.Function, Input: run.Input,
		Timeout: timeout, MemoryPages: pages, Resident: resident,
	}
	// An endpoint's answer is a whole response, such as a vault's sync.
	if run.Trigger == "endpoint" {
		call.MaxOutput = appEndpointAnswerMaxBytes
	}
	var output []byte
	var runErr error
	if long != nil {
		call.Timeout, call.Long = appLongRunLifetime, true
		output, runErr = runAppLongWorker(ctx, call, host.answer)
	} else {
		output, runErr = appWorkers.call(ctx, call, host.answer)
	}
	host.closeSockets()
	status, message := "ok", ""
	if runErr != nil {
		status, message = "failed", appRunFailure(runErr)
		if errors.Is(runErr, errAppWorkerHalted) {
			status = "halted"
		}
		if len(message) > 2000 {
			message = message[:2000]
		}
	}
	_, _ = app.DB().Update(appRunsTable, dbx.Params{
		"status": status, "error": message, "logs": host.logs.String(), "host_calls": host.calls,
		"duration_ms": time.Since(started).Milliseconds(), "finished": time.Now().UnixMilli(),
		"effects": host.effects.encoded(), "writes": host.effects.writes,
	}, dbx.HashExp{"id": runID}).Execute()
	return output, runID, runErr
}

// runWaitingAppHooks runs the workers of hook calls whose turn has come,
// a few at once; a failed call waits longer each time, five times in all.
func runWaitingAppHooks(ctx context.Context, app core.App, now time.Time) error {
	events := []struct {
		ID          string `db:"id"`
		InstallID   string `db:"install_id"`
		Hook        string `db:"hook"`
		Delivery    string `db:"delivery"`
		ContentType string `db:"content_type"`
		Body        []byte `db:"body"`
		Attempts    int    `db:"attempts"`
		ReceivedAt  int64  `db:"received_at"`
		Kind        string `db:"kind"`
		Topic       string `db:"topic"`
		PersonID    string `db:"person_id"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[event]].[[id]], [[event]].[[install_id]], [[event]].[[hook]], [[event]].[[delivery]],
			[[event]].[[content_type]], [[event]].[[body]], [[event]].[[attempts]], [[event]].[[received_at]],
			[[event]].[[kind]], [[event]].[[topic]], [[event]].[[person_id]]
		FROM {{leafen_app_hook_events}} [[event]] JOIN {{leafen_app_installs}} [[install]] ON [[install]].[[id]] = [[event]].[[install_id]]
		WHERE [[event]].[[status]] = 'waiting' AND [[event]].[[next_attempt_at]] <= {:now}
			AND [[install]].[[status]] = 'active' AND [[install]].[[removed_at]] = 0
		ORDER BY [[event]].[[received_at]] LIMIT {:limit}`).Bind(dbx.Params{"now": now.UnixMilli(), "limit": appWorkerLimits.atOnce * 2}).All(&events); err != nil {
		return err
	}
	var group sync.WaitGroup
	for _, event := range events {
		// A server stopping claims no more calls.
		if ctx.Err() != nil {
			break
		}
		claimed, err := app.DB().NewQuery(`UPDATE {{leafen_app_hook_events}} SET [[status]] = 'running'
			WHERE [[id]] = {:id} AND [[status]] = 'waiting'`).Bind(dbx.Params{"id": event.ID}).Execute()
		if err != nil {
			return err
		}
		if count, _ := claimed.RowsAffected(); count != 1 {
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			runErr := guardedError(app, "app call "+event.Kind+" "+event.Hook, func() error {
				install, err := loadAppInstall(app, event.InstallID)
				if err != nil {
					return err
				}
				_, manifest, err := loadAppPackage(app, install.PackageHash)
				if err != nil {
					return err
				}
				// A run names what it ran — the event, schedule or hook key — so its
				// list can say it by name (2026-10-16).
				run := appRun{Trigger: event.Kind, Ref: event.Hook}
				switch event.Kind {
				case appQueueEvent:
					declared, found := appManifestEventByKey(manifest, event.Hook)
					if !found {
						return fmt.Errorf("the app no longer has the event %s", event.Hook)
					}
					change := map[string]any{}
					_ = json.Unmarshal(event.Body, &change)
					workspaceID, _ := change["workspaceId"].(string)
					run.Worker, run.Function, run.Tenancy = declared.Worker, declared.Function, appWorkspaceTenancy(app, install.ID, workspaceID)
					run.Input, _ = json.Marshal(map[string]any{"event": event.Hook, "change": change, "tenancy": run.Tenancy, "receivedAt": event.ReceivedAt})
				case appQueueSchedule:
					declared, found := appManifestScheduleByKey(manifest, event.Hook)
					if !found {
						return fmt.Errorf("the app no longer has the schedule %s", event.Hook)
					}
					run.Worker, run.Function, run.Tenancy, run.Subject = declared.Worker, declared.Function, event.Topic, event.PersonID
					if declared.PerPerson {
						if run.Person, err = appPersonAuthority(app, install, manifest, event.Topic, event.PersonID); err != nil {
							return err
						}
					}
					run.Input, _ = json.Marshal(map[string]any{"schedule": event.Hook, "at": event.ReceivedAt, "tenancy": event.Topic,
						"person": event.PersonID})
				case appQueueAutomation:
					// A database automation's or button's call of an app action.
					action, err := appAutomationRunAction(app, install, event.Hook, event.PersonID, event.Topic)
					if err != nil {
						return err
					}
					call := struct {
						Input json.RawMessage   `json:"input"`
						Via   map[string]string `json:"via"`
					}{}
					var input any
					if json.Unmarshal(event.Body, &call) != nil || json.Unmarshal(call.Input, &input) != nil {
						return refuse(errAppInputInvalid, "the automation's call of %s holds no action input in JSON", event.Hook)
					}
					if err := validateAppActionInput(action, input); err != nil {
						return fmt.Errorf("the input doesn't fit the action: %w", err)
					}
					run.Worker, run.Function, run.Tenancy, run.Subject, run.Ref = action.Worker, action.Function, event.Topic, event.PersonID, action.Key
					if action.RunAs == "person" {
						if run.Person, err = appPersonAuthority(app, install, manifest, event.Topic, event.PersonID); err != nil {
							return err
						}
					}
					run.Input, _ = json.Marshal(map[string]any{"action": action.Key, "input": input, "person": event.PersonID, "tenancy": event.Topic,
						"via": call.Via})
				default:
					hook, found := appManifestHook(manifest, event.Hook)
					if !found {
						return fmt.Errorf("the app no longer has the hook %s", event.Hook)
					}
					// A call to an organization's address runs for it.
					run.Worker, run.Function, run.Tenancy = hook.Worker, hook.Function, event.Topic
					run.Input, _ = json.Marshal(map[string]any{"hook": event.Hook, "delivery": event.Delivery,
						"contentType": event.ContentType, "body": event.Body, "receivedAt": event.ReceivedAt, "tenancy": event.Topic})
				}
				_, _, err = runAppWorkerFunction(ctx, app, install, manifest, run)
				return err
			})
			finished := time.Now()
			if runErr == nil {
				_, _ = app.DB().Update(appHookEventsTable, dbx.Params{"status": "done", "error": "", "finished_at": finished.UnixMilli(),
					"attempts": event.Attempts + 1}, dbx.HashExp{"id": event.ID}).Execute()
				return
			}
			attempts := event.Attempts + 1
			next := dbx.Params{"status": "waiting", "attempts": attempts, "error": truncateAppRunError(runErr),
				"next_attempt_at": finished.Add(appHookRetryBase << (attempts - 1)).UnixMilli()}
			if errors.Is(runErr, errAppWorkerBusy) || errors.Is(runErr, errAppWorkerHalted) || ctx.Err() != nil {
				// Busy isn't a failure, nor an emergency stop or the server
				// stopping: the call keeps its attempts and waits — for a moment,
				// or until the app or the server runs again.
				next["attempts"], next["next_attempt_at"] = event.Attempts, finished.Add(appExecutorInterval).UnixMilli()
			} else if attempts >= appHookMaxAttempts {
				next["status"], next["finished_at"] = "failed", finished.UnixMilli()
			}
			_, _ = app.DB().Update(appHookEventsTable, next, dbx.HashExp{"id": event.ID}).Execute()
		}()
	}
	group.Wait()
	return nil
}

func appManifestEventByKey(manifest appManifest, key string) (appManifestEvent, bool) {
	for _, event := range manifest.Events {
		if event.Key == key {
			return event, true
		}
	}
	return appManifestEvent{}, false
}

func appManifestScheduleByKey(manifest appManifest, key string) (appManifestSchedule, bool) {
	for _, schedule := range manifest.Schedules {
		if schedule.Key == key {
			return schedule, true
		}
	}
	return appManifestSchedule{}, false
}

func appWorkspaceTenancyKnown(app core.App, installID, tenancy string) bool {
	var count int
	_ = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_workspaces}} WHERE [[install_id]] = {:install} AND [[tenancy_key]] = {:tenancy}`).
		Bind(dbx.Params{"install": installID, "tenancy": tenancy}).Row(&count)
	return tenancy != "" && count > 0
}

func truncateAppRunError(err error) string {
	message := err.Error()
	if len(message) > 2000 {
		return message[:2000]
	}
	return message
}

// startAppWorkerExecutor runs waiting hook calls as they come and ends idle
// or stopped apps' workers, until the server stops. Calls cut off by a
// restart wait again.
func startAppWorkerExecutor(app core.App, background *appPlatformBackground) {
	_, _ = app.DB().NewQuery(`UPDATE {{leafen_app_hook_events}} SET [[status]] = 'waiting' WHERE [[status]] = 'running'`).Execute()
	_, _ = app.DB().NewQuery(`UPDATE {{leafen_app_runs}} SET [[status]] = 'failed', [[error]] = 'server_restarted'
		WHERE [[status]] = 'running'`).Execute()
	background.run(func(ctx context.Context) {
		ticker := time.NewTicker(appExecutorInterval)
		defer ticker.Stop()
		lastSweep, lastMinute := time.Now(), time.Time{}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-appHookWake:
			}
			if now := time.Now(); now.Truncate(time.Minute) != lastMinute {
				lastMinute = now.Truncate(time.Minute)
				if err := queueDueAppSchedules(app, now); err != nil {
					app.Logger().Warn("leafen app schedules failed", "error", err)
				}
			}
			if err := runWaitingAppHooks(ctx, app, time.Now()); err != nil {
				app.Logger().Warn("leafen app hooks run failed", "error", err)
			}
			if time.Since(lastSweep) > time.Minute {
				lastSweep = time.Now()
				appKV.sweep(time.Now())
				appWorkers.endIdle(time.Now(), func(installID string) bool {
					install, err := loadAppInstall(app, installID)
					return err != nil || install.Status != "active" || install.RemovedAt != 0
				})
			}
		}
	})
}

// appRunFailure is what a failed run's record keeps: a code its screen puts
// in words (AppJobsDialog), never a worker's own text. An app's refusal keeps
// its reason, the app's words to the person.
func appRunFailure(err error) string {
	var past appWorkerPastLimit
	var refusal appWorkerRefusal
	text := err.Error()
	switch {
	case errors.As(err, &refusal):
		return "refused:" + refusal.Reason
	case errors.As(err, &past):
		return "past_limit:" + strings.TrimPrefix(past.Text, "the call ran past ")
	case errors.Is(err, errAppWorkerBusy):
		return "busy"
	case errors.Is(err, errAppWorkerStopped):
		return "worker_stopped"
	case strings.HasPrefix(text, "the output is larger than ") && strings.HasSuffix(text, " bytes"):
		// The worker's own words, from its process (app_platform_worker_child.go).
		return "output_too_large:" + strings.TrimSuffix(strings.TrimPrefix(text, "the output is larger than "), " bytes")
	}
	return "failed"
}
