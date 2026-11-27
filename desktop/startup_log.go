package main

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
)

// The app's own log, beside its data folder (startup_recovery.go reads it
// on a computer).

const (
	startupLogName = "desktop.log"
	// Past this the log starts afresh, the one before kept beside it.
	startupLogLimit = 1 << 20
	// How much of the log's end a report carries.
	startupReportTail = 64 << 10
)

// startupLogPath is the app's log, beside its data folder as the first
// start's choices are, so that a restore leaves it.
func startupLogPath() (string, error) {
	dir, err := localDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(dir), startupLogName), nil
}

// openStartupLog writes the app's log to its file as well.
func openStartupLog() {
	path, err := startupLogPath()
	if err != nil {
		return
	}
	if info, err := os.Stat(path); err == nil && info.Size() > startupLogLimit {
		_ = os.Rename(path, path+".1")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	log.SetOutput(startupLogWriter{file})
	// A crash's report goes there too: a Windows app started from Explorer
	// has no standard error to read it from.
	_ = debug.SetCrashOutput(file, debug.CrashOptions{})
}

// startupLogWriter writes the log to its file first, then to standard error
// where there is one: a Windows window program has none, and a writer that
// failed there first wrote nothing to the file (2026-11-09: an empty
// desktop.log for an app Explorer started).
type startupLogWriter struct{ file *os.File }

func (writer startupLogWriter) Write(line []byte) (int, error) {
	written, err := writer.file.Write(line)
	_, _ = os.Stderr.Write(line)
	return written, err
}

// fileTail is a file's last limit bytes, from a line's start.
func fileTail(path string, limit int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	start := max(0, info.Size()-limit)
	raw := make([]byte, info.Size()-start)
	if _, err := file.ReadAt(raw, start); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	text := string(raw)
	if start > 0 {
		if cut := strings.IndexByte(text, '\n'); cut >= 0 {
			text = text[cut+1:]
		}
	}
	return text, nil
}
