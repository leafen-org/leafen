package app

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// How far a long backup, restore or backup file has got as it runs (user
// 2026-11-22: a first backup to S3 ran 46 minutes, its status saying only
// that it ran): its step, and the bytes and files done of all it has, for
// the status the screen and agents read. One backup or restore runs at a
// time (core.StoreKeyActiveBackup) and one backup file is made at a time, so
// each keeps one record in memory from its start to its end.
const (
	backupProgressStoreKey     = "leafenBackupProgress"
	backupFileProgressStoreKey = "leafenBackupFileProgress"
)

type backupProgress struct {
	mu   sync.Mutex
	view administratorBackupProgress
	// The backup file's job, for the job's status.
	jobID string
}

type backupProgressContextKey struct{}

// startBackupProgress begins the record a status reads under key, at the
// first step of its kind: opening the place and the database for a backup
// or a restore, PocketBase packing the data for a backup file.
func startBackupProgress(app core.App, key, kind, jobID string) *backupProgress {
	step := backupProgressStepDatabase
	if kind == backupProgressKindFile {
		step = backupProgressStepArchive
	}
	progress := &backupProgress{jobID: jobID, view: administratorBackupProgress{
		Kind: kind, Step: step, StartedAt: time.Now().UTC().Format(time.RFC3339),
	}}
	app.Store().Set(key, progress)
	return progress
}

// endBackupProgress takes the record away and answers it as it ended.
func endBackupProgress(app core.App, key string, progress *backupProgress) administratorBackupProgress {
	if current, ok := app.Store().Get(key).(*backupProgress); ok && current == progress {
		app.Store().Remove(key)
	}
	progress.mu.Lock()
	defer progress.mu.Unlock()
	progress.view.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	return progress.viewLocked()
}

// runningBackupProgress is the record of what runs under key, if any.
func runningBackupProgress(app core.App, key string) *backupProgress {
	progress, _ := app.Store().Get(key).(*backupProgress)
	return progress
}

func withBackupProgress(ctx context.Context, progress *backupProgress) context.Context {
	return context.WithValue(ctx, backupProgressContextKey{}, progress)
}

// backupProgressFrom is the record a backup writes to, or nil where none
// follows it (a server moving out, the command line).
func backupProgressFrom(ctx context.Context) *backupProgress {
	progress, _ := ctx.Value(backupProgressContextKey{}).(*backupProgress)
	return progress
}

// snapshot is the record as the status shows it now; nil for none.
func (progress *backupProgress) snapshot() *administratorBackupProgress {
	if progress == nil {
		return nil
	}
	progress.mu.Lock()
	defer progress.mu.Unlock()
	view := progress.viewLocked()
	return &view
}

// viewLocked keeps the counts whole: content that grew as it was read counts
// in the total too. A step with no total known keeps none.
func (progress *backupProgress) viewLocked() administratorBackupProgress {
	view := progress.view
	if view.BytesTotal > 0 {
		view.BytesTotal = max(view.BytesTotal, view.BytesDone)
	}
	if view.FilesTotal > 0 {
		view.FilesTotal = max(view.FilesTotal, view.FilesDone)
	}
	return view
}

// step names the step it is at, with what that step has in all, which the
// totals gain.
func (progress *backupProgress) step(step string, bytes int64, files int) {
	if progress == nil {
		return
	}
	progress.mu.Lock()
	defer progress.mu.Unlock()
	progress.view.Step = step
	progress.view.BytesTotal += bytes
	progress.view.FilesTotal += files
}

// restart names a step counted on its own, from nothing done of what it
// has in all.
func (progress *backupProgress) restart(step string, bytes int64, files int) {
	progress.add(func(view *administratorBackupProgress) {
		view.Step, view.BytesTotal, view.FilesTotal, view.BytesDone, view.FilesDone = step, bytes, files, 0, 0
	})
}

// total adds to what there is in all, as it becomes known.
func (progress *backupProgress) total(bytes int64, files int) {
	progress.add(func(view *administratorBackupProgress) {
		view.BytesTotal += bytes
		view.FilesTotal += files
	})
}

// done adds what was gone through: read and sent, or found already there.
func (progress *backupProgress) done(bytes int64, files int) {
	progress.add(func(view *administratorBackupProgress) {
		view.BytesDone += bytes
		view.FilesDone += files
	})
}

// skipped counts a file gone from the data folder while the run went
// through it: done, and left out of the snapshot.
func (progress *backupProgress) skipped(bytes int64) {
	progress.add(func(view *administratorBackupProgress) {
		view.BytesDone += bytes
		view.FilesDone++
		view.FilesSkipped++
	})
}

// sent adds the content new at the place, which went over to it.
func (progress *backupProgress) sent(bytes int64) {
	progress.add(func(view *administratorBackupProgress) { view.BytesSent += bytes })
}

// setDone says how much is done of a step whose work is seen from outside.
func (progress *backupProgress) setDone(bytes int64) {
	progress.add(func(view *administratorBackupProgress) { view.BytesDone = bytes })
}

func (progress *backupProgress) add(change func(view *administratorBackupProgress)) {
	if progress == nil {
		return
	}
	progress.mu.Lock()
	defer progress.mu.Unlock()
	change(&progress.view)
}

// reader counts what is read through it as done.
func (progress *backupProgress) reader(source io.Reader) io.Reader {
	if progress == nil {
		return source
	}
	return &backupProgressReader{source: source, progress: progress, passes: 1}
}

// passReader and passWriter count what goes through them as one of two
// passes over the same bytes: half each, so the step's total is the bytes
// themselves.
func (progress *backupProgress) passReader(source io.Reader) io.Reader {
	if progress == nil {
		return source
	}
	return &backupProgressReader{source: source, progress: progress, passes: 2}
}

func (progress *backupProgress) passWriter(target io.Writer) io.Writer {
	if progress == nil {
		return target
	}
	return &backupProgressWriter{target: target, progress: progress, passes: 2}
}

type backupProgressReader struct {
	source   io.Reader
	progress *backupProgress
	passes   int64
	carried  int64
}

func (reader *backupProgressReader) Read(buffer []byte) (int, error) {
	read, err := reader.source.Read(buffer)
	reader.carried += int64(read)
	reader.progress.done(reader.carried/reader.passes, 0)
	reader.carried %= reader.passes
	return read, err
}

type backupProgressWriter struct {
	target   io.Writer
	progress *backupProgress
	passes   int64
	carried  int64
}

func (writer *backupProgressWriter) Write(buffer []byte) (int, error) {
	written, err := writer.target.Write(buffer)
	writer.carried += int64(written)
	writer.progress.done(writer.carried/writer.passes, 0)
	writer.carried %= writer.passes
	return written, err
}

// What the operations list tells agents of the progress the status gives.
func init() {
	const restoreProgress = "The backup is put together apart from the request: it answers once it is, or after 20 seconds with " +
		"schedule.progress (kind restore). Then follow GET /admin/backups/schedule with wait_seconds: progress while it goes; " +
		"after it, restorePending (the server starts again by itself where restoreRestart is automatic) or lastRestore: " +
		"where it stopped, its errorCode, errorReason (what the place or the system said) and errorCause (that answer's common cause)."
	const fileProgress = "While the file is made, job.progress says its step — archive (PocketBase packing the data: bytesDone " +
		"packed so far, no total), write, check, keep — and bytesDone of bytesTotal where a step has a total. A failed one has job.errorCode " +
		"and job.errorReason, what the system said."
	for id, note := range map[string]string{
		"GET /admin/backups/schedule": "While a backup or a restore runs, progress says how far it has got: kind (backup or restore), " +
			"step (database, files, finishing), bytesDone of bytesTotal and filesDone of filesTotal gone through, and bytesSent: " +
			"what went over to the place, new there. lastRun is the last backup as it ended: the step it got to (done when it " +
			"finished, with errorCode, errorReason — what the place or the system said — and errorCause, that answer's common cause, when it failed), what it went through " +
			"and sent, filesSkipped (files deleted while it ran, left out), startedAt and finishedAt; lastRestore a " +
			"restore that stopped after its request was answered.",
		"POST /admin/backups/schedule/run": "Starts a backup now. While one is already running nothing more starts: it answers " +
			"200 with alreadyRunning true and that run's schedule.progress — say it is already backing up. Turning backups on, " +
			"or sending them elsewhere, with PUT /admin/backups/schedule starts the first backup at once: its answer has backupStarted true.",
		"POST /admin/backups/schedule/restore": restoreProgress,
		"POST /admin/backups/replica/restore":  restoreProgress,
		"GET /admin/backups/current":           fileProgress,
		"GET /admin/backups/{jobId}":           fileProgress,
	} {
		nativeOperationNotes[nativeOperationShape(id)] = note
	}
}
