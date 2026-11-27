package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The backup screen's "이 파일로 복원하려면" gives the command that restores
// a downloaded backup where this server runs (docs/self-hosting.md
// "Restore"), the screen filling in its file and SHA-256: in Leafen's image,
// through Docker Compose; in the desktop app, the app's own program, which
// restores into the app's data on a computer whose app has none yet;
// elsewhere this program, into a new data folder. A phone app's own server
// has no terminal to type one in, so the screen gives no command there and
// says where it restores instead: a backup chosen on the screen, the file on
// a computer, the whole server moved without one (2026-11-21: it gave the
// app's program on the phone, which nothing there runs).
const (
	administratorBackupRestoreContainer = "container"
	administratorBackupRestoreApp       = "app"
	administratorBackupRestoreServer    = "server"
	administratorBackupRestorePhone     = "phone"
)

// administratorBackupRestoreWay is where this server runs, for its restore
// command, and its program as a terminal takes it ("" in a container, whose
// program is the image's, and on a phone, which has no terminal).
func administratorBackupRestoreWay(app core.App) (string, string) {
	switch {
	case runtime.GOOS == "ios" || runtime.GOOS == "android":
		return administratorBackupRestorePhone, ""
	case runningInContainer() || strings.TrimSpace(os.Getenv(containerRuntimeEnvironment)) == "1":
		return administratorBackupRestoreContainer, ""
	case app.Store().Has(localOwnerStoreKey):
		return administratorBackupRestoreApp, typedServerProgram()
	}
	return administratorBackupRestoreServer, typedServerProgram()
}

// typedServerProgram is this program as a terminal takes it, as the desktop
// app types its own (desktop/program_typed.go): its full path, quoted where
// it holds a space or a quote; on Windows after PowerShell's "&", without
// its extension, so that the app's console program beside it runs.
func typedServerProgram() string {
	program, err := os.Executable()
	if err != nil {
		return "leafen"
	}
	if runtime.GOOS == "windows" {
		return "& " + psQuoted(strings.TrimSuffix(program, filepath.Ext(program)))
	}
	if strings.ContainsAny(program, " '\"&;()") {
		return "'" + strings.ReplaceAll(program, "'", `'\''`) + "'"
	}
	return program
}
