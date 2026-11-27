//go:build darwin && !ios

package main

import (
	"errors"
	"flag"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runUpdateHelper switches the Mac app to the staged version once the app
// with the given process has ended (updater_darwin.go): it keeps a copy of
// the data and the old app, puts the new app in place, starts it and waits
// for it to come up; when it doesn't, it puts the old app and the data back
// and starts that.
func runUpdateHelper(args []string) int {
	flags := flag.NewFlagSet("apply-update", flag.ContinueOnError)
	pid := flags.Int("pid", 0, "the app's process to wait for")
	background := flags.Bool("background", false, "start the app behind its window, as it was opened at login")
	if flags.Parse(args) != nil || *pid <= 0 {
		return 2
	}
	dir, err := updatesDir()
	if err != nil {
		return 1
	}
	staged := &StagedUpdate{}
	if !readJSON(filepath.Join(dir, updateStagedFile), staged) {
		return 1
	}
	// An app that hasn't ended keeps its version; the switch waits for the
	// next start.
	if !waitForExit(*pid, 2*time.Minute) {
		return 1
	}
	defer removeOwnCopy()
	data, err := localDataDir()
	if err != nil {
		return 1
	}
	// Only the last switch's copy is kept: the old app and the data as they
	// were before it.
	keep := filepath.Join(dir, "rollback", staged.From)
	_ = os.RemoveAll(filepath.Join(dir, "rollback"))
	if err := os.MkdirAll(filepath.Join(keep, "data"), 0o700); err != nil {
		return 1
	}
	// The data as the old version left it, before the new one changes its
	// database.
	for _, name := range []string{"data.db", "data.db-wal", "data.db-shm"} {
		if _, err := os.Stat(filepath.Join(data, name)); err == nil {
			if err := copyFile(filepath.Join(data, name), filepath.Join(keep, "data", name), 0o600); err != nil {
				return 1
			}
		}
	}
	old := filepath.Join(keep, filepath.Base(staged.Target))
	if err := swapInto(staged.Target, staged.Payload, old); err != nil {
		return 1
	}
	_ = os.Remove(filepath.Join(dir, updateStagedFile))
	_ = writeJSON(filepath.Join(dir, updatePendingHealth), staged)
	healthy := filepath.Join(dir, "healthy-"+staged.Version)
	updating := filepath.Join(dir, "updating-"+staged.Version)
	_ = os.Remove(healthy)
	_ = os.Remove(updating)
	defer os.Remove(updating)
	started, err := startInstalled(staged.Target, *background)
	if err == nil {
		// Two minutes to come up, and on while its server says it is
		// bringing the data up to date (MarkUpdating), up to half an hour.
		began := time.Now()
		stillUpdating := func() bool {
			info, err := os.Stat(updating)
			return err == nil && time.Since(info.ModTime()) < updateProgressFresh && time.Since(began) < updateHealthWaitUpdating
		}
		for deadline := began.Add(updateHealthWait); time.Now().Before(deadline) || stillUpdating(); time.Sleep(time.Second) {
			if _, err := os.Stat(healthy); err == nil {
				_ = os.Remove(healthy)
				_ = os.Remove(filepath.Join(dir, updatePendingHealth))
				_ = os.RemoveAll(filepath.Join(dir, staged.Version))
				return 0
			}
			if started.ProcessState != nil {
				break
			}
		}
		if started.Process != nil {
			_ = started.Process.Kill()
			waitForExit(started.Process.Pid, 30*time.Second)
		}
	}
	// The new version didn't come up: the old app and its data come back.
	failed := filepath.Join(dir, "failed-"+staged.Version)
	_ = os.RemoveAll(failed)
	_ = swapInto(staged.Target, old, failed)
	for _, name := range []string{"data.db", "data.db-wal", "data.db-shm"} {
		_ = os.Remove(filepath.Join(data, name))
		if _, err := os.Stat(filepath.Join(keep, "data", name)); err == nil {
			_ = copyFile(filepath.Join(keep, "data", name), filepath.Join(data, name), 0o600)
		}
	}
	_ = os.Remove(filepath.Join(dir, updatePendingHealth))
	_ = os.RemoveAll(filepath.Join(dir, staged.Version))
	_ = writeJSON(filepath.Join(dir, updateSkipFile), map[string]string{"version": staged.Version})
	_ = writeJSON(filepath.Join(dir, updateRolledBack), RolledBack{Version: staged.Version, Reason: "the new version didn't start"})
	_, _ = startInstalled(staged.Target, *background)
	return 0
}

// waitForExit waits for a process to end, answering whether it did.
func waitForExit(pid int, limit time.Duration) bool {
	for deadline := time.Now().Add(limit); processAlive(pid); time.Sleep(200 * time.Millisecond) {
		if time.Now().After(deadline) {
			return false
		}
	}
	return true
}

// removeOwnCopy removes the helper's copy of the program once it is done;
// the app removes a copy left behind the next time it starts.
func removeOwnCopy() {
	if executable, err := os.Executable(); err == nil && strings.HasPrefix(filepath.Base(executable), "update-helper") {
		_ = os.Remove(executable)
	}
}

// swapInto puts incoming in target's place and moves what was there to
// outgoing. Whatever has to be copied between disks is copied while the app
// is still whole: incoming goes beside target first, the switch itself is
// two renames in one folder, and the old app leaves after (2026-10-18: moved
// out first, a power cut while the new app was copied in from another disk
// left no app at all).
func swapInto(target, incoming, outgoing string) error {
	coming, going := target+updateSiblingNew, target+updateSiblingOld
	_ = os.RemoveAll(coming)
	_ = os.RemoveAll(going)
	if err := moveAside(incoming, coming); err != nil {
		return err
	}
	if err := os.Rename(target, going); err != nil {
		_ = moveAside(coming, incoming)
		return err
	}
	if err := os.Rename(coming, target); err != nil {
		_ = os.Rename(going, target)
		_ = moveAside(coming, incoming)
		return err
	}
	return moveAside(going, outgoing)
}

// moveAside moves a file or folder, copying when the two places are on
// different disks.
func moveAside(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	if err := copyTree(from, to); err != nil {
		return err
	}
	return os.RemoveAll(from)
}

func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

// startInstalled starts the installed app's program — behind its window
// when the app it replaces had been opened at login.
func startInstalled(target string, background bool) (*exec.Cmd, error) {
	entries, err := os.ReadDir(filepath.Join(target, "Contents", "MacOS"))
	if err != nil || len(entries) == 0 {
		return nil, errors.New("the app holds no program")
	}
	// Its output goes nowhere rather than into a pipe that would close with
	// the helper.
	command := exec.Command(filepath.Join(target, "Contents", "MacOS", entries[0].Name()))
	if background {
		command.Args = append(command.Args, "--background")
	}
	command.Env = os.Environ()
	detach(command)
	if err := command.Start(); err != nil {
		return nil, err
	}
	go func() { _ = command.Wait() }()
	return command, nil
}
