package app

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The standby's settings and screen (replica_standby.go): the primary's
// connection code, joining as its standby and leaving, taking over by itself
// or now, the primary letting its standby go, and what the screen shows.

// createStandbyCode gives a primary's administrator the code a standby
// joins with. The primary must be replicating.
func createStandbyCode(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	stored, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	if !replicationWanted(stored) {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupInputInvalid, "Turn on continuous replication first.")
	}
	if stored.Target == scheduledBackupTargetFolder && syncFolderReplicaPlace(stored.Folder) {
		return standbyPlaceSlowError(event)
	}
	pairSecret, err := ensureReplicaPairSecret(event.App.DataDir())
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't make the code.", err)
	}
	// Each code joins on its own; making one again closes the ones before.
	joinToken, err := rotateReplicaJoinToken(event.App.DataDir())
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't make the code.", err)
	}
	code := standbyCode{PairSecret: pairSecret, JoinToken: joinToken, Target: stored.Target, Folder: stored.Folder, WebDAVURL: stored.WebDAVURL, WebDAVUsername: stored.WebDAVUsername,
		S3Endpoint: stored.S3Endpoint, S3Region: stored.S3Region, S3Bucket: stored.S3Bucket, S3Prefix: stored.S3Prefix,
		S3AccessKey: stored.S3AccessKey, S3PathStyle: stored.S3PathStyle, PrimaryOrigin: configuredPublicOrigin(),
		PrimaryVersion: ReleaseVersion()}
	switch stored.Target {
	case scheduledBackupTargetWebDAV:
		code.WebDAVPassword, err = scheduledBackupWebDAVPassword(event.App, stored)
	case scheduledBackupTargetS3:
		code.S3SecretKey, err = scheduledBackupS3Secret(event.App, stored)
	}
	if err == nil {
		var secret scheduledBackupSecret
		if secret, err = scheduledBackupSecretOf(event.App, stored); err == nil {
			code.Password = secret.Password
			if secret.Record != nil {
				code.Password, code.BackupKey, code.BackupKeyRecord = "", hex.EncodeToString(secret.Master), string(mustJSON(*secret.Record))
			}
		}
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	raw, err := json.Marshal(code)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't make the code.", err)
	}
	return event.JSON(http.StatusOK, administratorStandbyCode{Code: standbyCodePrefix + base64.RawURLEncoding.EncodeToString(raw)})
}

// standbyPlaceSlowError refuses a pair at a place that shows one server's
// changes to the other late (replica_pair_check.go).
func standbyPlaceSlowError(event *core.RequestEvent) error {
	return apiError(event, http.StatusConflict, errorCodeStandbyPlaceSlow,
		"A standby needs a backup place both servers see at once, such as S3 or a shared drive; a cloud sync folder shows changes too late.")
}

func decodeStandbyCode(raw string) (standbyCode, error) {
	code := standbyCode{}
	encoded, ok := strings.CutPrefix(strings.TrimSpace(raw), standbyCodePrefix)
	if !ok {
		return code, errors.New("not a standby code")
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || json.Unmarshal(data, &code) != nil {
		return code, errors.New("not a standby code")
	}
	if code.Target != scheduledBackupTargetFolder && code.Target != scheduledBackupTargetWebDAV && code.Target != scheduledBackupTargetS3 {
		return code, errors.New("not a standby code")
	}
	return code, nil
}

// joinAsStandby makes this server the standby of the primary whose code is
// given: the place's replica must be there and open with the code's
// password. This server's own data is replaced when it takes over.
func joinAsStandby(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	input := administratorStandbyJoinInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeStandbyCodeInvalid, "That isn't a standby code.")
	}
	if strings.TrimSpace(input.Code) == "" {
		return refuseMissingInput(event, errorCodeStandbyCodeInvalid, "code", "the standby code the main server shows.")
	}
	code, err := decodeStandbyCode(input.Code)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeStandbyCodeInvalid, "That isn't a standby code.")
	}
	current, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	// A moved server's own replication is over: it may follow the server that took its data.
	if current.SetByEnvironment || (replicationWanted(current) && readReplicaFence(event.App.DataDir()) == nil) {
		return apiError(event, http.StatusConflict, errorCodeStandbyNotFresh, "This server replicates its own data; it can't follow another.")
	}
	if NewerReleaseVersion(code.PrimaryVersion, ReleaseVersion()) {
		return apiError(event, http.StatusConflict, errorCodeStandbyVersionOlder, "This server is older than the primary; update it first.")
	}
	if code.Target == scheduledBackupTargetFolder && syncFolderReplicaPlace(code.Folder) {
		return standbyPlaceSlowError(event)
	}
	holder, err := loadReplicaHolder(event.App.DataDir())
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read this server's mark.", err)
	}
	place := storedScheduledBackup{Target: code.Target, Folder: code.Folder, WebDAVURL: code.WebDAVURL, WebDAVUsername: code.WebDAVUsername,
		S3Endpoint: code.S3Endpoint, S3Region: code.S3Region, S3Bucket: code.S3Bucket, S3Prefix: code.S3Prefix,
		S3AccessKey: code.S3AccessKey, S3PathStyle: code.S3PathStyle}
	place.WebDAVPassword, _ = encryptAdministratorSettingIfSet(event.App, code.WebDAVPassword)
	place.S3Secret, _ = encryptAdministratorSettingIfSet(event.App, code.S3SecretKey)
	place.PasswordCipher, _ = encryptAdministratorSettingIfSet(event.App, code.Password)
	place.MasterCipher, _ = encryptAdministratorSettingIfSet(event.App, code.BackupKey)
	place.KeyRecord = code.BackupKeyRecord
	secret, err := scheduledBackupSecretOf(event.App, place)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeStandbyCodeInvalid, "That isn't a standby code.")
	}
	store, err := openScheduledBackupStore(event.Request.Context(), event.App, place)
	if err != nil {
		return apiErrorFrom(event, http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't reach the backup place.", err)
	}
	source, err := readReplicaSource(store, secret)
	if err != nil {
		closeScheduledBackupStore(store)
		return apiErrorFrom(event, http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't reach the backup place.", err)
	}
	if source.owner == nil || !source.readable {
		closeScheduledBackupStore(store)
		return apiError(event, http.StatusUnprocessableEntity, errorCodeReplicaUnavailable, "The primary's replica isn't there, or doesn't open.")
	}
	// The place must carry a mark to the primary and its answer back in
	// time, or the standby would judge the primary by an old beat.
	probed := probeReplicaPlace(event.Request.Context(), store, holder.Holder, code.JoinToken)
	closeScheduledBackupStore(store)
	if errors.Is(probed, errReplicaJoinRefused) {
		return apiError(event, http.StatusBadRequest, errorCodeStandbyCodeInvalid, "The primary has made a newer connection code; use that one.")
	}
	if errors.Is(probed, errReplicaPlaceSlow) {
		return standbyPlaceSlowError(event)
	}
	if probed != nil {
		return apiError(event, http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't reach the backup place.")
	}
	// A key's call asks its person once the primary's replica opens and its
	// place answers in time (user_key_acting_ask.go): the mark it carried is
	// taken away again, and nothing here has changed yet.
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	holder.PairSecret = code.PairSecret
	if err := saveReplicaHolder(event.App.DataDir(), holder); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the standby setting.", err)
	}
	if _, err := event.App.DB().NewQuery(`
		UPDATE {{leafen_scheduled_backups}} SET [[revision]] = [[revision]] + 1, [[enabled]] = 0, [[target]] = {:target},
			[[folder]] = {:folder}, [[webdav_url]] = {:webdavURL}, [[webdav_username]] = {:webdavUsername},
			[[webdav_password_cipher]] = {:webdavCipher}, [[s3_endpoint]] = {:s3Endpoint}, [[s3_region]] = {:s3Region},
			[[s3_bucket]] = {:s3Bucket}, [[s3_prefix]] = {:s3Prefix}, [[s3_access_key]] = {:s3AccessKey},
			[[s3_secret_cipher]] = {:s3Cipher}, [[s3_path_style]] = {:s3PathStyle}, [[password_cipher]] = {:passwordCipher},
			[[replication_enabled]] = 0, [[standby_enabled]] = 1, [[standby_auto_takeover]] = 1,
			[[standby_primary_origin]] = {:origin}, [[backup_master_cipher]] = {:masterCipher}, [[backup_key_record]] = {:keyRecord}
		WHERE [[id]] = 'default'
	`).Bind(dbx.Params{"target": place.Target, "folder": place.Folder, "webdavURL": place.WebDAVURL,
		"webdavUsername": place.WebDAVUsername, "webdavCipher": place.WebDAVPassword, "s3Endpoint": place.S3Endpoint,
		"s3Region": place.S3Region, "s3Bucket": place.S3Bucket, "s3Prefix": place.S3Prefix, "s3AccessKey": place.S3AccessKey,
		"s3Cipher": place.S3Secret, "s3PathStyle": place.S3PathStyle, "passwordCipher": place.PasswordCipher,
		"origin": code.PrimaryOrigin, "masterCipher": place.MasterCipher, "keyRecord": place.KeyRecord}).Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the standby setting.", err)
	}
	_ = os.RemoveAll(standbyFolder(event.App))
	notifyReplicationRunner(event.App)
	return answerSchedule(event)
}

func encryptAdministratorSettingIfSet(app core.App, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	return encryptAdministratorSetting(app, cipherScheduledBackup, value)
}

// leaveStandby stops following: the server is a server of its own again.
func leaveStandby(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	if _, err := event.App.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[standby_enabled]] = 0,
		[[revision]] = [[revision]] + 1 WHERE [[id]] = 'default'`).Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the standby setting.", err)
	}
	notifyReplicationRunner(event.App)
	return answerSchedule(event)
}

// setStandbyAutoTakeover turns taking over by itself on or off.
func setStandbyAutoTakeover(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	input := administratorReplicationInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeScheduledBackupInputInvalid, "The standby setting is not valid.")
	}
	if _, err := event.App.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[standby_auto_takeover]] = {:on},
		[[revision]] = [[revision]] + 1 WHERE [[id]] = 'default'`).Bind(dbx.Params{"on": input.Enabled}).Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorSettingsSaveFailed, "Couldn't save the standby setting.", err)
	}
	return answerSchedule(event)
}

// takeOverNow asks the standby to take over at once.
func takeOverNow(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	runner, ok := event.App.Store().Get(replicationRunnerStoreKey).(*replicationRunner)
	stored, err := loadScheduledBackup(event.App)
	if !ok || err != nil || !stored.StandbyEnabled {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupInputInvalid, "This server isn't a standby.")
	}
	// A key's call asks its person once this server is a standby
	// (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	select {
	case runner.takeover <- struct{}{}:
	default:
	}
	return answerSchedule(event)
}

// dismissStandby lets the primary's standby go: it stops following once it
// reads the note.
func dismissStandby(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	stored, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	store, err := openScheduledBackupStore(event.Request.Context(), event.App, stored)
	if err != nil {
		return apiErrorFrom(event, http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't reach the backup place.", err)
	}
	defer closeScheduledBackupStore(store)
	standby := &replicaStandbyBeat{}
	if present, err := readReplicaJSON(store, replicaStandbyName, standby); err == nil && present {
		_ = writeReplicaJSON(store, standbyDismissName, map[string]string{"holder": standby.Holder})
	}
	_ = store.RemoveAll(scheduledBackupStoreJoin(replicaRoot(), replicaStandbyName))
	_ = store.RemoveAll(scheduledBackupStoreJoin(replicaRoot(), replicaLeaseName))
	if runner, ok := event.App.Store().Get(replicationRunnerStoreKey).(*replicationRunner); ok {
		runner.lease.forget()
		runner.noteDirect("")
	}
	return answerSchedule(event)
}

func answerSchedule(event *core.RequestEvent) error {
	saved, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	return event.JSON(http.StatusOK, administratorScheduledBackupResult{Schedule: projectScheduledBackupWithRestore(event.App, saved)})
}

// projectStandby is the pair's part of the backups' screen: this server as
// a standby following its primary, or as a primary with a standby, and a
// takeover it made.
func projectStandby(app core.App, stored storedScheduledBackup) administratorStandby {
	projected := administratorStandby{Role: standbyRoleNone, Version: ReleaseVersion(), AutoTakeover: stored.StandbyAutoTakeover}
	runner, ok := app.Store().Get(replicationRunnerStoreKey).(*replicationRunner)
	switch {
	case stored.StandbyEnabled:
		projected.Role, projected.PeerOrigin = standbyRoleStandby, stored.StandbyPrimaryOrigin
		if ok {
			status := runner.standbyStatus()
			projected.PeerOrigin = firstNonEmpty(status.PrimaryOrigin, stored.StandbyPrimaryOrigin)
			projected.PeerVersion, projected.TakingOver, projected.ErrorCode = status.PrimaryVersion, status.TakingOver, status.ErrorCode
			projected.PeerSilentSeconds = int(status.PrimaryStill.Seconds())
			projected.Followed = int64(status.Followed)
			projected.DirectCheck = status.DirectCheck
			if !status.FollowedAt.IsZero() {
				projected.FollowedAt = status.FollowedAt.UTC().Format(time.RFC3339)
			}
		}
	case ok:
		standby, still, paused := runner.lease.snapshot(time.Now())
		projected.Paused = paused
		if standby != nil {
			projected.Role, projected.PeerOrigin, projected.PeerVersion = standbyRolePrimary, standby.Origin, standby.Version
			projected.PeerSilentSeconds, projected.Followed = int(still.Seconds()), int64(standby.Followed)
			projected.DirectCheck = runner.directCheck()
		}
	}
	work := restoreWorkDir(app.DataDir())
	takeover := map[string]any{}
	for _, name := range []string{standbyTakeoverName, "announced-" + standbyTakeoverName} {
		if readRestoreFile(filepath.Join(work, name), &takeover) == nil {
			if at, ok := takeover["at"].(float64); ok {
				projected.TookOverAt = time.UnixMilli(int64(at)).UTC().Format(time.RFC3339)
			}
			projected.TookOverFrom, _ = takeover["from"].(string)
			break
		}
	}
	return projected
}
