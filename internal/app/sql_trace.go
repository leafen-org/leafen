package app

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

// sqlTraceDirEnvironment names a directory where a server writes each
// different statement its data database runs, with its values as "?", one per
// line, to a file named for the process: those of its two pools here, and
// those of read snapshots, whose pool takes these pools' log as it opens
// (read_snapshot.go). A query-plan audit reads them to find statements that
// scan a growing table. It is off unless set.
const sqlTraceDirEnvironment = "LEAFEN_SQL_TRACE_DIR"

// sqlTraceMigrating is set while a start runs and checks the schema
// migrations, whose statements read whole tables once by design; the trace
// writes those to a file of their own so the audit keeps them apart.
var sqlTraceMigrating atomic.Bool

// The values the database log writes into a statement: quoted text, bytes
// as hex, a missing value and numbers; and its table and column names as the
// query builder writes them before quoting.
var (
	sqlTraceNames   = regexp.MustCompile(`\{\{([^}]+)\}\}|\[\[([^\]]+)\]\]`)
	sqlTraceStrings = regexp.MustCompile(`'(?:[^']|'')*'`)
	sqlTraceValues  = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b|<nil>|\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b`)
	sqlTraceLists   = regexp.MustCompile(`(?i)\bIN\s*\(\s*\?(?:\s*,\s*\?)*\s*\)`)
)

// sqlTraceStatement is a statement as the trace keeps it: on one line, with
// its names quoted, its values as "?" and a list of them as one.
func sqlTraceStatement(statement string) string {
	statement = strings.Join(strings.Fields(statement), " ")
	statement = sqlTraceNames.ReplaceAllStringFunc(statement, func(name string) string {
		parts := strings.Split(strings.Trim(name, "{}[]"), ".")
		for index, part := range parts {
			parts[index] = "`" + strings.TrimSpace(part) + "`"
		}
		return strings.Join(parts, ".")
	})
	statement = sqlTraceStrings.ReplaceAllString(statement, "?")
	statement = sqlTraceValues.ReplaceAllString(statement, "?")
	return sqlTraceLists.ReplaceAllString(statement, "IN (?)")
}

func registerSQLTrace(pb *pocketbase.PocketBase) {
	dir := strings.TrimSpace(os.Getenv(sqlTraceDirEnvironment))
	if dir == "" {
		return
	}
	pb.OnBootstrap().BindFunc(func(event *core.BootstrapEvent) error {
		if err := event.Next(); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		files := [2]*os.File{}
		for kind, name := range []string{"sql-%d.log", "sql-%d-migration.log"} {
			file, err := os.OpenFile(
				filepath.Join(dir, fmt.Sprintf(name, os.Getpid())), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600,
			)
			if err != nil {
				return err
			}
			files[kind] = file
		}
		var mutex sync.Mutex
		seen := [2]map[string]struct{}{{}, {}}
		write := func(statement string) {
			kind := 0
			if sqlTraceMigrating.Load() {
				kind = 1
			}
			line := sqlTraceStatement(statement)
			mutex.Lock()
			defer mutex.Unlock()
			if _, done := seen[kind][line]; done {
				return
			}
			seen[kind][line] = struct{}{}
			_, _ = files[kind].WriteString(line + "\n")
		}
		for _, builder := range []dbx.Builder{event.App.ConcurrentDB(), event.App.NonconcurrentDB()} {
			db, ok := builder.(*dbx.DB)
			if !ok {
				continue
			}
			db.QueryLogFunc = func(_ context.Context, _ time.Duration, statement string, _ *sql.Rows, _ error) {
				write(statement)
			}
			db.ExecLogFunc = func(_ context.Context, _ time.Duration, statement string, _ sql.Result, _ error) {
				write(statement)
			}
		}
		return nil
	})
}
