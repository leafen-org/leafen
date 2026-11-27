package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// One server serves a data folder at a time (2026-11-06: two `serve`
// processes on one folder each kept their own live copy of a page, so a line
// typed through one landed under another heading once both wrote, and both
// ran the backup schedule and the replica). `serve` holds a lock file in the
// folder for as long as it runs, taken before anything in the folder is
// opened or moved; a second `serve` stops there and says so. Commands that
// read or copy the data beside a running server (backup, doctor, mcp) take
// no lock, and the desktop app, one window to a computer, starts its server
// without this command.
const serverDataLockName = ".leafen-serve.lock"

var errServerDataInUse = errors.New("another Leafen server is serving it; stop that one first, or give this one its own folder with --dir")

// serveCommandLine is set when the command line runs `serve` (Execute).
var serveCommandLine bool

var serverDataLock struct {
	mu   sync.Mutex
	dir  string
	file *os.File
}

func registerServerDataLock(pb *pocketbase.PocketBase) {
	pb.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{
		Id: "leafenServerDataLock",
		// Right after the retired-data refusal, which leaves such a folder
		// as it is, and before the database opens or a restore moves in.
		Priority: -998,
		Func: func(event *core.BootstrapEvent) error {
			if serveCommandLine {
				if err := holdServerDataLock(event.App.DataDir()); err != nil {
					return fmt.Errorf("the data folder %s: %w", event.App.DataDir(), err)
				}
			}
			return event.Next()
		},
	})
}

// holdServerDataLock takes the folder's lock for the rest of the process; the
// system lets it go when the process ends, however it ends.
func holdServerDataLock(dataDir string) error {
	serverDataLock.mu.Lock()
	defer serverDataLock.mu.Unlock()
	if serverDataLock.file != nil && serverDataLock.dir == dataDir {
		return nil
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dataDir, serverDataLockName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, held, err := tryExclusiveBackupFileLock(file)
	if err != nil || !held {
		_ = file.Close()
		if err == nil {
			err = errServerDataInUse
		}
		return err
	}
	if serverDataLock.file != nil {
		_ = serverDataLock.file.Close()
	}
	serverDataLock.dir, serverDataLock.file = dataDir, file
	return nil
}
