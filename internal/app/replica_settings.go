package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Continuous replication's settings and screen (replica_runner.go): a switch
// beside the backups, the replica's state, what a place's replica holds, and
// for a server whose data moved elsewhere, keeping it apart. Migration 546
// keeps the switch and when administrators last heard the replica failing.

// registerReplication starts the replica with the server, leaves this
// machine's parts out of one-off backups, and refuses changes once the
// server's data moved elsewhere.
func registerReplication(pb core.App, runner *replicationRunner) {
	pb.OnBackupCreate().BindFunc(func(event *core.BackupEvent) error {
		// The read log's gathered records go in (read_log_buffer.go).
		flushReadLog(event.App)
		for name := range scheduledBackupMachineOnly {
			event.Exclude = append(event.Exclude, name)
		}
		return event.Next()
	})
	pb.OnServe().BindFunc(func(event *core.ServeEvent) error {
		event.Router.BindFunc(refuseChangesWhenMoved)
		event.Router.PUT(apiRoot+"/admin/backups/replication", updateReplication)
		event.Router.POST(apiRoot+"/admin/backups/replication/apart", keepServerApart)
		event.Router.GET(apiRoot+"/admin/backups/replica", describeReplica)
		event.Router.POST(apiRoot+"/admin/backups/replica/restore", requestReplicaRestore)
		event.Router.POST(apiRoot+"/admin/backups/standby/code", createStandbyCode)
		event.Router.DELETE(apiRoot+"/admin/backups/standby", dismissStandby)
		event.Router.PUT(apiRoot+"/admin/backups/standby/join", joinAsStandby)
		event.Router.DELETE(apiRoot+"/admin/backups/standby/join", leaveStandby)
		event.Router.PUT(apiRoot+"/admin/backups/standby/auto", setStandbyAutoTakeover)
		event.Router.POST(apiRoot+"/admin/backups/standby/takeover", takeOverNow)
		event.Router.GET(apiRoot+replicaPairCheckPath, readReplicaPairCheck)
		runner.Start()
		serverBackgroundOf(event.App).run("standby takeover mail", func(context.Context) { announceTakeover(event.App) })
		return event.Next()
	})
}

// refuseChangesWhenMoved answers every change with where the data went
// once another server took this one's replica over. Reading, signing in, a
// person's own display settings (so the first screen after signing in goes
// on to the band saying where the data went) and the backup settings,
// where an administrator keeps the server apart, still work.
func refuseChangesWhenMoved(event *core.RequestEvent) error {
	method := event.Request.Method
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return event.Next()
	}
	fence := readReplicaFence(event.App.DataDir())
	if fence == nil {
		if replicationPaused(event.App) && !changeAllowedWhileStopped(event.Request.URL.Path) {
			return apiError(event, http.StatusServiceUnavailable, errorCodeServerPaused,
				"This server can't reach its backup place, where its standby may take over; changes wait until it can.")
		}
		return event.Next()
	}
	if changeAllowedWhileStopped(event.Request.URL.Path) {
		return event.Next()
	}
	return apiError(event, http.StatusServiceUnavailable, errorCodeServerMoved,
		"This server's data moved to another server; changes here aren't kept.")
}

// changeAllowedWhileStopped is a change a moved or paused server still
// takes: signing in, a person's own display settings, the backup settings.
func changeAllowedWhileStopped(path string) bool {
	return strings.Contains(path, "/auth-with-") || strings.Contains(path, "/auth-refresh") ||
		strings.HasPrefix(path, apiRoot+"/auth/") || strings.HasPrefix(path, apiRoot+"/admin/backups/") ||
		path == apiRoot+"/account/display-preferences" || path == apiRoot+"/account/language"
}

// projectReplication is the replica's part of the backups' screen.
func projectReplication(app core.App, stored storedScheduledBackup) administratorReplication {
	status := replicationStatusOf(app)
	environmentSet, environmentOn := replicationFromEnvironment()
	projected := administratorReplication{
		Enabled: stored.ReplicationEnabled, SetByEnvironment: environmentSet, State: status.State,
		ErrorCode: status.ErrorCode, MovedTo: status.MovedTo,
	}
	if environmentSet {
		projected.Enabled = environmentOn
	}
	if !status.LastSyncAt.IsZero() {
		projected.LastSyncAt = status.LastSyncAt.UTC().Format(time.RFC3339)
	}
	if !status.BehindSince.IsZero() {
		projected.BehindSeconds = int(time.Since(status.BehindSince).Seconds())
	}
	return projected
}

// updateReplication turns the replica on or off. On needs backups on with
// a place, and a place whose replica, if any, is this server's.
func updateReplication(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	input := administratorReplicationInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeScheduledBackupInputInvalid, "The replication setting is not valid.")
	}
	if set, _ := replicationFromEnvironment(); set {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupSetByEnvironment, "The server's environment sets replication.")
	}
	stored, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	if input.Enabled && (!stored.Enabled || !stored.targetSet()) {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupInputInvalid, "Turn on backups first.")
	}
	if input.Enabled && !stored.ReplicationEnabled {
		store, err := openScheduledBackupStore(event.Request.Context(), event.App, stored)
		if err != nil {
			return apiErrorFrom(event, http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't reach the backup place.", err)
		}
		owner, err := readReplicaOwner(store)
		closeScheduledBackupStore(store)
		if err != nil {
			return apiErrorFrom(event, http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't reach the backup place.", err)
		}
		holder, err := loadReplicaHolder(event.App.DataDir())
		if err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the replication setting.", err)
		}
		if owner != nil && owner.Holder != holder.Holder {
			return apiError(event, http.StatusConflict, errorCodeReplicaOwnedElsewhere,
				"Another server's replica is in this place. Restore from it, or choose another place.")
		}
		// Turned on again, a replica gone from the place starts afresh.
		if owner == nil && holder.Claimed != "" {
			holder.Claimed = ""
			if err := saveReplicaHolder(event.App.DataDir(), holder); err != nil {
				return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the replication setting.", err)
			}
		}
	}
	if _, err := event.App.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[replication_enabled]] = {:enabled},
		[[replication_alerted_at]] = 0, [[revision]] = [[revision]] + 1 WHERE [[id]] = 'default'`).
		Bind(dbx.Params{"enabled": input.Enabled}).Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the replication setting.", err)
	}
	notifyReplicationRunner(event.App)
	saved, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	return event.JSON(http.StatusOK, administratorScheduledBackupResult{Schedule: projectScheduledBackupWithRestore(event.App, saved)})
}

// keepServerApart lets a server whose data moved elsewhere take changes
// again as a server of its own: its replication goes off, so it never
// writes the replica the other server holds.
func keepServerApart(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	if _, err := event.App.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[replication_enabled]] = 0,
		[[revision]] = [[revision]] + 1 WHERE [[id]] = 'default'`).Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the replication setting.", err)
	}
	if err := os.Remove(replicaFencePath(event.App)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the replication setting.", err)
	}
	// The answer already shows the server apart.
	if runner, ok := event.App.Store().Get(replicationRunnerStoreKey).(*replicationRunner); ok {
		runner.setStatus(func(status *replicationStatus) { *status = replicationStatus{State: replicationStateOff} })
	}
	notifyReplicationRunner(event.App)
	saved, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	return event.JSON(http.StatusOK, administratorScheduledBackupResult{Schedule: projectScheduledBackupWithRestore(event.App, saved)})
}

func replicaFencePath(app core.App) string { return filepath.Join(app.DataDir(), replicaFencedName) }

// alertReplicationFailing tells administrators once a day, while it goes
// on, that changes no longer reach the replica.
func alertReplicationFailing(app core.App, code string, now time.Time) error {
	var alertedAt int64
	if err := app.DB().NewQuery(`SELECT [[replication_alerted_at]] FROM {{leafen_scheduled_backups}} WHERE [[id]] = 'default'`).
		Row(&alertedAt); err != nil {
		return err
	}
	if alertedAt > 0 && now.Sub(time.UnixMilli(alertedAt)) < scheduledBackupAlertEvery {
		return nil
	}
	settings, err := loadAdministratorSettings(app)
	if err != nil || !notificationEmailConfigured(settings) {
		return err
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[replication_alerted_at]] = {:now} WHERE [[id]] = 'default'`).
		Bind(dbx.Params{"now": now.UnixMilli()}).Execute(); err != nil {
		return err
	}
	return mailServerAdministrators(app, settings, "replication-alert-failed", func(recipient, language string) []byte {
		var subject, body string
		if language == accountLanguageKo {
			subject = "리펜 연속 복제가 멈췄습니다"
			body = "리펜의 변경이 1분 넘게 복제본에 반영되지 않고 있습니다(" + scheduledBackupAlertReason(code, true) + ").\r\n" +
				"설정 → 서버 → 백업에서 백업 위치를 확인해 주세요. 복제가 다시 되면 이 알림은 멈춥니다.\r\n"
		} else {
			subject = "Leafen’s continuous replication stopped"
			body = "Leafen’s changes haven’t reached the replica for over a minute (" + scheduledBackupAlertReason(code, false) + ").\r\n" +
				"Check the backup location in Settings → Server → Backups. These mails stop once replication works again.\r\n"
		}
		if origin := configuredPublicOrigin(); origin != "" {
			body += "\r\n" + origin + "\r\n"
		}
		return serverAdministratorMail(settings, recipient, subject, body)
	})
}

// describeReplica tells what the backup place's replica holds: whose it
// is, when it last changed, and whether the backups' password opens it.
func describeReplica(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	stored, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	if !stored.targetSet() {
		return event.JSON(http.StatusOK, administratorReplicaResult{Replica: administratorReplica{Owner: replicaOwnerNone}})
	}
	source, err := openReplicaSource(event.Request.Context(), event.App, stored)
	if err != nil {
		return apiErrorFrom(event, http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't read the backup place.", err)
	}
	defer source.close()
	return event.JSON(http.StatusOK, administratorReplicaResult{Replica: source.describe(event.Request.Context(), event.App)})
}
