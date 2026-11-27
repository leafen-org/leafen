package app

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"modernc.org/libc"
	sqlite3 "modernc.org/sqlite/lib"
)

// SQLite's lookaside (decided 2026-10-17, measured for 100,000 people on one
// server): modernc's SQLite takes every allocation through one process-wide
// lock, and at 3,000 people nine tenths of the waiting was there. Each
// connection's own lookaside of small slots takes most allocations off that
// lock; 2048-byte slots, 1500 a connection, doubled the most requests a
// second the server could take. A server sizes it from its memory limit — a
// tenth of it over every connection it may open, so a small NAS takes fewer
// slots — once, before any connection opens. The app's own server, for one
// person with few connections, keeps SQLite's default.

const (
	sqliteLookasideSlot     = 2048
	sqliteLookasideMaxSlots = 1500
	sqliteLookasideMinSlots = 100
)

var sqliteLookasideOnce sync.Once

// sqliteLookaside is what was set: slot size, slots a connection, SQLite's
// answer, or why it kept its default.
var sqliteLookaside struct {
	Slot   int    `json:"slot"`
	Slots  int    `json:"slots"`
	Result int32  `json:"result"`
	Kept   string `json:"kept,omitempty"`
}

// registerSQLiteLookaside sets the lookaside first thing at bootstrap,
// before the database opens. A server says its memory plan, the settings it
// could not use and the lookaside in its output as it starts (a container's
// log among them); a command typed on its data (backup, account, connect …)
// says none of it, where a terminal shows only what the command answers
// (2026-11-19: every such command printed these lines on standard error,
// which PowerShell shows as a red error). `leafen doctor` reports them all.
func registerSQLiteLookaside(pb core.App) {
	pb.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{
		Id:       "leafenSQLiteLookaside",
		Priority: -2000,
		Func: func(event *core.BootstrapEvent) error {
			sqliteLookasideOnce.Do(func() {
				out := io.Discard
				if processServes(event.App) {
					out = os.Stderr
				}
				plan := serverMemoryPlan()
				fmt.Fprintf(out, "leafen: memory plan: %d MiB from %s; page cache %d KiB a connection, map %d MiB, %d data and %d snapshot connections\n",
					plan["limitMiB"], plan["from"], plan["pageCacheKiB"], plan["memoryMapMiB"], plan["dataConnections"], plan["snapshotConnections"])
				for _, refusal := range settingRefusals() {
					fmt.Fprintf(out, "leafen: %s\n", refusal)
				}
				configureSQLiteLookaside(event.App, out)
			})
			return event.Next()
		},
	})
}

// processServes says whether this process runs the server: the `serve`
// command (Execute) or the desktop app's own server (local_owner.go), not a
// command on its data.
func processServes(app core.App) bool {
	return serveCommandLine || app.Store().Has(localOwnerStoreKey)
}

func configureSQLiteLookaside(app core.App, out io.Writer) {
	if app.Store().Has(localOwnerStoreKey) {
		sqliteLookaside.Kept = "the app's own server"
		fmt.Fprintln(out, "leafen: sqlite lookaside: default kept (the app's own server)")
		return
	}
	slots, limit, source := plannedSQLiteLookaside()
	tls := libc.NewTLS()
	defer tls.Close()
	sqlite3.Xsqlite3_shutdown(tls)
	arguments := libc.NewVaList(int32(sqliteLookasideSlot), int32(slots))
	result := sqlite3.Xsqlite3_config(tls, sqlite3.SQLITE_CONFIG_LOOKASIDE, arguments)
	libc.Xfree(tls, arguments)
	if initialized := sqlite3.Xsqlite3_initialize(tls); result == 0 {
		result = initialized
	}
	sqliteLookaside.Slot, sqliteLookaside.Slots, sqliteLookaside.Result = sqliteLookasideSlot, slots, result
	if result != 0 {
		sqliteLookaside.Kept = "SQLite refused it"
	}
	fmt.Fprintf(out, "leafen: sqlite lookaside: %d slots of %d bytes a connection (memory limit %d MiB from %s), result %d\n",
		slots, sqliteLookasideSlot, limit/memoryMiB, source, result)
}

// plannedSQLiteLookaside is the slots a connection a server takes, from its
// memory limit and where that came from.
func plannedSQLiteLookaside() (int, int64, string) {
	limit, source := serverMemoryLimit()
	slots := int(limit / 10 / int64(sqliteConnections()*sqliteLookasideSlot))
	return min(max(slots, sqliteLookasideMinSlots), sqliteLookasideMaxSlots), limit, source
}
