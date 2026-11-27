package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

// An update that failed isn't tried again by the same build on the same data
// (2026-11-22, the decision on Docker updates). `leafen serve` is started
// again as soon as it stops — by Docker's restart: unless-stopped, a NAS's
// container manager or a service manager —, and each start ran the same
// migrations into the same failure and stopped, over and over. Now a serve
// whose update fails notes the failure beside the recovery points,
// backups/migrations/leafen_update_failed.json, and stays up saying so with
// the way back — the release it ran before, and the data to start it on —
// until it is stopped (health and the page, schema_migration_answer.go; the
// log). Started again, the same build on the same data doesn't try the
// update again: it says it failed, and waits.
//
// It is tried again
//   - by another build: a newer release, or this one built again (its
//     program file is another), as a new image is;
//   - by this build once another one has served the data since (builds.json):
//     the release from before was used, and the update was asked for again,
//     on data that changed (the try keeps it again, schema_migration_recovery.go);
//   - by any start once the file is gone.
//
// The desktop and phone apps and the other commands try again as before: a
// person starts them, and the apps' recovery screen tries again once there
// is room on the disk.

const updateFailureFile = "leafen_update_failed.json"

// updateFailure is a serve's update that failed on its data.
type updateFailure struct {
	// The build that failed: its name, its program file's time, and its
	// migrations' digest.
	Build      string `json:"build"`
	Release    string `json:"release"`
	Program    string `json:"program,omitempty"`
	Migrations string `json:"migrations"`
	FromSchema int    `json:"fromSchema"`
	ToSchema   int    `json:"toSchema"`
	// DataSchema is the schema the failure left the data at: FromSchema
	// unless it failed after the migrations were applied.
	DataSchema int    `json:"dataSchema"`
	Code       string `json:"code"`
	Error      string `json:"error"`
	FailedAt   string `json:"failedAt"`
	// The build the data ran before, and the recovery point of the update
	// in backups/migrations/, when there is one.
	PreviousBuild   string `json:"previousBuild,omitempty"`
	PreviousRelease string `json:"previousRelease,omitempty"`
	DataBefore      string `json:"dataBefore,omitempty"`

	// runs is how the server runs (administratorBackupRestoreWay); again,
	// that this start found the failure rather than tried the update.
	runs  string
	again bool
}

// holdFailedUpdate is set for `leafen serve` (Execute): a serve whose update
// failed stays up saying so (waitOnFailedUpdate).
var holdFailedUpdate bool

var heldUpdateFailure struct {
	mu      sync.Mutex
	failure *updateFailure
}

// currentUpdateFailure is the failure of the update this serve start tried
// or found, nil for none.
func currentUpdateFailure() *updateFailure {
	heldUpdateFailure.mu.Lock()
	defer heldUpdateFailure.mu.Unlock()
	return heldUpdateFailure.failure
}

func holdUpdateFailure(failure *updateFailure) {
	heldUpdateFailure.mu.Lock()
	heldUpdateFailure.failure = failure
	heldUpdateFailure.mu.Unlock()
}

func updateFailurePath(dataDir string) string {
	return filepath.Join(dataDir, core.LocalBackupsDirName, "migrations", updateFailureFile)
}

// updateFailureBuild names this build as a failure keeps it.
func updateFailureBuild() (build string, program string) {
	if written := serverProgramTime(); !written.IsZero() {
		program = written.UTC().Format(time.RFC3339Nano)
	}
	return serverBuildName(serverBuildStampOf()), program
}

// migrationFailureCode is the error code a failed update answers with.
func migrationFailureCode(err error) string {
	for _, known := range []string{errorCodeSchemaMigrationHistoryInvalid, errorCodeSchemaMigrationRecoveryFailed, errorCodeSchemaMigrationFailed} {
		if strings.HasPrefix(err.Error(), known) {
			return known
		}
	}
	return errorCodeSchemaMigrationFailed
}

// earlierUpdateFailure is this build's failed update of the data, when it
// isn't to be tried again: the same build, program and migrations, the same
// target, the data where the failure left it, and no build served it since.
func earlierUpdateFailure(app core.App, migrations string, toSchema int, dataSchema int) (*updateFailure, bool) {
	raw, err := os.ReadFile(updateFailurePath(app.DataDir()))
	if err != nil {
		return nil, false
	}
	failure := &updateFailure{}
	if json.Unmarshal(raw, failure) != nil {
		return nil, false
	}
	build, program := updateFailureBuild()
	failedAt, err := time.Parse(time.RFC3339Nano, failure.FailedAt)
	if err != nil || failure.Build != build || failure.Program != program || failure.Migrations != migrations ||
		failure.ToSchema != toSchema || failure.DataSchema != dataSchema {
		return nil, false
	}
	for _, seen := range readServerBuildsSeen(app) {
		if seen.LastStartedAt > failedAt.UnixMilli() {
			return nil, false
		}
	}
	failure.runs, _ = administratorBackupRestoreWay(app)
	failure.again = true
	return failure, true
}

// noteUpdateFailure keeps the failure of this start's update.
func noteUpdateFailure(app core.App, migrations string, fromSchema int, toSchema int, cause error) *updateFailure {
	build, program := updateFailureBuild()
	code := migrationFailureCode(cause)
	failure := &updateFailure{Build: build, Release: ReleaseVersion(), Program: program, Migrations: migrations,
		FromSchema: fromSchema, ToSchema: toSchema, DataSchema: fromSchema, Code: code,
		Error:    strings.TrimPrefix(strings.TrimPrefix(cause.Error(), code), ": "),
		FailedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	_ = app.DB().NewQuery(`SELECT COALESCE(MAX([[version]]), 0) FROM {{leafen_schema_migrations}}`).Row(&failure.DataSchema)
	if previous, ok := previousServerBuild(app, build); ok {
		failure.PreviousBuild, failure.PreviousRelease = previous.Build, previous.Release
	}
	root := filepath.Join(app.DataDir(), core.LocalBackupsDirName, "migrations")
	if kept, err := newestMigrationRecoveryPoint(root, migrationRecoveryName(fromSchema, toSchema)); err == nil && kept.name != "" {
		failure.DataBefore = kept.name
		if kept.zip {
			failure.DataBefore += ".zip"
		}
	}
	failure.runs, _ = administratorBackupRestoreWay(app)
	err := os.MkdirAll(root, 0o700)
	if err == nil {
		err = writeSetupFile(root, updateFailureFile, failure)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "leafen: the failed update couldn't be noted, so a start again tries it again: %v\n", err)
	}
	return failure
}

// forgetUpdateFailure lets a noted failure go once an update passed.
func forgetUpdateFailure(dataDir string) {
	_ = os.Remove(updateFailurePath(dataDir))
}

// againError is the start's error for a failure it found.
func (failure *updateFailure) againError() error {
	return fmt.Errorf("%s: this build's update of the data from schema %d to %d failed at %s and isn't tried again on the same data: %s",
		failure.Code, failure.FromSchema, failure.ToSchema, failure.FailedAt, failure.Error)
}

// previous is the release to go back to, "" when the data's builds don't
// name one other than this release.
func (failure *updateFailure) previous() string {
	if failure.PreviousRelease == failure.Release {
		return ""
	}
	return failure.PreviousRelease
}

// dataChanged says whether the failure came after the migrations changed
// the data.
func (failure *updateFailure) dataChanged() bool {
	return failure.DataSchema != failure.FromSchema
}

// updateWayBackWords are the way back's words, English and Korean.
var updateWayBackWords = map[string][2]string{
	"containerRelease": {
		"To go back, run Leafen %[1]s again: LEAFEN_IMAGE=ghcr.io/leafen-org/leafen:%[1]s in the .env beside compose.yaml, then docker compose up -d (in Container Manager or Portainer, the %[1]s tag).",
		"되돌리려면 리펜 %[1]s 버전을 다시 실행하세요. compose.yaml 옆 .env에 LEAFEN_IMAGE=ghcr.io/leafen-org/leafen:%[1]s 줄을 적고 docker compose up -d를 실행합니다(Container Manager나 Portainer에서는 %[1]s 태그를 고릅니다).",
	},
	"container": {
		"To go back, run the image you ran before again: its tag as LEAFEN_IMAGE in the .env beside compose.yaml, then docker compose up -d (in Container Manager or Portainer, that tag).",
		"되돌리려면 전에 쓰던 이미지를 다시 실행하세요. compose.yaml 옆 .env의 LEAFEN_IMAGE에 그 태그를 적고 docker compose up -d를 실행합니다(Container Manager나 Portainer에서는 그 태그를 고릅니다).",
	},
	"serverRelease": {
		"To go back, start Leafen %s again on this data folder.",
		"되돌리려면 이 데이터 폴더로 리펜 %s 버전을 다시 시작하세요.",
	},
	"server": {
		"To go back, start the version you ran before again on this data folder.",
		"되돌리려면 이 데이터 폴더로 전에 쓰던 버전을 다시 시작하세요.",
	},
	"unchanged": {
		"The data is as it was before this update.",
		"데이터는 이번 업데이트 전 그대로입니다.",
	},
	"changedKept": {
		"This update changed the data: start it on a copy of backups/migrations/%s in the data folder, as Upgrades in the self-hosting guide says.",
		"이번 업데이트가 데이터를 바꿨으니, 데이터 폴더의 backups/migrations/%s 복사본으로 시작하세요(자체 호스팅 안내의 Upgrades).",
	},
	"changed": {
		"This update changed the data: start it on the backup made before the update.",
		"이번 업데이트가 데이터를 바꿨으니, 업데이트 전에 만든 백업으로 시작하세요.",
	},
	"again": {
		"This version won’t try the update again on its own; a newer one will.",
		"이 버전은 업데이트를 스스로 다시 시도하지 않습니다. 더 새 버전이 나오면 다시 시도합니다.",
	},
}

// wayBack says how to go back from the failure, in English (0) or Korean
// (1), as fits how the server runs.
func (failure *updateFailure) wayBack(language int) string {
	word := func(key string) string { return updateWayBackWords[key][language] }
	kind := "server"
	if failure.runs == administratorBackupRestoreContainer {
		kind = "container"
	}
	sentences := []string{word(kind)}
	if previous := failure.previous(); previous != "" {
		sentences[0] = fmt.Sprintf(word(kind+"Release"), previous)
	}
	switch {
	case !failure.dataChanged():
		sentences = append(sentences, word("unchanged"))
	case failure.DataBefore != "":
		sentences = append(sentences, fmt.Sprintf(word("changedKept"), failure.DataBefore))
	default:
		sentences = append(sentences, word("changed"))
	}
	return strings.Join(append(sentences, word("again")), " ")
}

// addUpdateFailure gives health's failed answer the failure and the way
// back, in English.
func addUpdateFailure(body map[string]any, failure *updateFailure) {
	body["fromSchema"], body["toSchema"] = failure.FromSchema, failure.ToSchema
	body["failedAt"], body["dataChanged"], body["wayBack"] = failure.FailedAt, failure.dataChanged(), failure.wayBack(0)
	if previous := failure.previous(); previous != "" {
		body["previousRelease"] = previous
	}
	if failure.DataBefore != "" {
		body["dataBefore"] = "backups/migrations/" + failure.DataBefore
	}
}

// waitOnFailedUpdate keeps a serve whose update failed up, saying so, until
// it is stopped: ended, Docker's restart policy or a service manager would
// start it again at once.
func waitOnFailedUpdate(pb *pocketbase.PocketBase, failure *updateFailure) {
	// What opened lets go of the data, for the release from before.
	_ = pb.ClearBootstrap()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	happened := "didn't finish"
	if failure.again {
		happened = "failed here at " + failure.FailedAt + " and isn't tried again"
	}
	said := "This server waits until it is stopped."
	if answer := currentMigrationAnswer(); answer != nil {
		said = "This server waits until it is stopped, and " + serveEarlyAddress + " says so."
	}
	fmt.Fprintf(os.Stderr, "leafen: the update to %s, from data schema %d to %d, %s: %s: %s\nleafen: %s %s\n",
		failure.Build, failure.FromSchema, failure.ToSchema, happened, failure.Code, failure.Error, said, failure.wayBack(0))
	<-stop
	if answer := currentMigrationAnswer(); answer != nil {
		answer.close()
	}
}
