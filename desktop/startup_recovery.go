package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"strings"
	"time"

	"leafen.org/leafen/internal/app"
)

// The startup recovery screen's side in the app (user decision 2026-10-21):
// the app's own log, which the screen saves; what it copies for help; the
// data folder it opens; and the last backup it restores while the server is
// off. The window asks for these only when the server couldn't start or is
// slow to.

// StartupRecovery is what the window's startup recovery screen asks of the
// app.
type StartupRecovery struct {
	local *localServer
}

// Report is what the screen copies or saves for help: the app's version and
// system, its data folder, why its server couldn't start or that it is
// still starting, its last backup, and the end of its log.
func (recovery *StartupRecovery) Report() string {
	var report strings.Builder
	fmt.Fprintf(&report, "Leafen %s on %s/%s\n", app.ReleaseVersion(), runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&report, "Time: %s\n", time.Now().Format(time.RFC3339))
	dir, dirErr := localDataDir()
	if dirErr != nil {
		fmt.Fprintf(&report, "Data folder: unavailable (%v)\n", dirErr)
	} else {
		fmt.Fprintf(&report, "Data folder: %s\n", dir)
	}
	ready, _, _, _ := recovery.local.current()
	select {
	case <-ready:
		if _, err, _, _ := recovery.local.current(); err != nil {
			fmt.Fprintf(&report, "Couldn't start: %v\n", err)
			if reason := app.StartupFailureReason(dir, err); reason != "" {
				fmt.Fprintf(&report, "Reason: %s\n", reason)
			}
		} else {
			report.WriteString("Started\n")
		}
	default:
		report.WriteString("Still starting\n")
	}
	if dirErr == nil {
		if made, ok := app.LastBackup(dir); ok {
			fmt.Fprintf(&report, "Last backup: %s\n", made.Format(time.RFC3339))
		} else {
			report.WriteString("Last backup: none noted\n")
		}
	}
	if path, err := startupLogPath(); err == nil {
		if tail, err := fileTail(path, startupReportTail); err == nil {
			fmt.Fprintf(&report, "\n--- %s ---\n%s", startupLogName, tail)
		}
	}
	return report.String()
}

// CanOpenDataFolder says whether this device shows a folder in its file
// manager: a computer does, a phone doesn't.
func (recovery *StartupRecovery) CanOpenDataFolder() bool {
	return dataFolderOpens
}

// OpenDataFolder shows the app's data folder in Finder, Explorer or the
// file manager.
func (recovery *StartupRecovery) OpenDataFolder() error {
	dir, err := localDataDir()
	if err != nil {
		return err
	}
	return openDataFolder(dir)
}

// RestoreLastBackup puts the server's last backup in its data's place and
// starts the server on it, while the server is off because it couldn't
// start; the data held now stays aside to undo the restore. A sealed backup
// the data folder doesn't open opens with secret, its recovery code or its
// password; without one that does, it answers "needs-secret".
func (recovery *StartupRecovery) RestoreLastBackup(secret string) (string, error) {
	ready, _, _, _ := recovery.local.current()
	select {
	case <-ready:
	default:
		return "", errors.New("the server is still starting")
	}
	if _, err, _, _ := recovery.local.current(); err == nil {
		return "", errors.New("the server is running")
	}
	dir, err := localDataDir()
	if err != nil {
		return "", err
	}
	err = app.RestoreLastBackup(context.Background(), dir, secret, "")
	if secret != "" && errors.Is(err, app.ErrBackupNeedsSecret) {
		err = app.RestoreLastBackup(context.Background(), dir, "", secret)
	}
	if errors.Is(err, app.ErrBackupNeedsSecret) {
		log.Printf("restoring the last backup needs its recovery code or password: %v", err)
		return "needs-secret", nil
	}
	if err != nil {
		log.Printf("restoring the last backup failed: %v", err)
		return "", err
	}
	log.Print("restoring the last backup: starting the server again on it")
	// A start that fails on it shows on the screen again, with why.
	_ = recovery.local.restart()
	return "restored", nil
}
