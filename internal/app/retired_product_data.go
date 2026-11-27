package app

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	sqlite3 "modernc.org/sqlite/lib"
)

// Leafen began as Hanji and does not carry Hanji's data forward (the Leafen
// identity cutover): a data folder, backup, folder snapshot or workspace
// archive that Hanji wrote is refused as Hanji's, and left as it is, rather
// than read as if Leafen had written it. These are the only places Leafen
// still names Hanji.
const (
	retiredProductName         = "Hanji"
	retiredProductTablePrefix  = "hanji_"
	retiredProductFormatPrefix = "hanji."
	retiredProductArchiveRoot  = "hanji/"
)

var errRetiredProductData = errors.New("this was made by " + retiredProductName +
	", whose data Leafen does not open; start Leafen with new data")

// isRetiredProductFormat says whether a manifest's format names Hanji's.
func isRetiredProductFormat(format string) bool {
	return strings.HasPrefix(format, retiredProductFormatPrefix)
}

// isRetiredProductArchiveEntry says whether a workspace archive entry is one
// Hanji wrote (its archive kept everything under "hanji/").
func isRetiredProductArchiveEntry(name string) bool {
	return strings.HasPrefix(filepath.ToSlash(name), retiredProductArchiveRoot)
}

// registerRetiredProductDataRefusal stops the server before it opens a data
// folder it refuses — one Hanji used, or one from before the schema baseline
// (data_folder_doctor.go): after a pending restore is applied (that restore
// may itself bring such data) and before PocketBase opens the database,
// which would write to it.
func registerRetiredProductDataRefusal(pb *pocketbase.PocketBase) {
	pb.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{
		Id:       "leafenRetiredProductDataRefusal",
		Priority: -999,
		Func: func(event *core.BootstrapEvent) error {
			if err := dataFolderRefusal(event.App.DataDir()); err != nil {
				return err
			}
			return event.Next()
		},
	})
}

// retiredProductDataRefusal is why a data folder Hanji used is refused, as
// the server's start and `leafen doctor` say it, or nil for any other.
func retiredProductDataRefusal(dataDir string) error {
	retired, err := isRetiredProductDataFolder(dataDir)
	if err != nil {
		return fmt.Errorf("checking which product made the data folder %s: %w", dataDir, err)
	}
	if retired {
		return fmt.Errorf("the data folder %s: %w (the folder is left as it is)", dataDir, errRetiredProductData)
	}
	return nil
}

// isRetiredProductDataFolder reads the folder's database without writing
// anything, not even the index SQLite keeps beside a write-ahead log: the
// database file is read as unchanging, and a table Hanji made that is still
// only in the log is found by its CREATE statement in the log's pages.
func isRetiredProductDataFolder(dataDir string) (bool, error) {
	path := filepath.Join(dataDir, "data.db")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	database, err := openDatabaseUnchanged(path)
	if err != nil {
		return false, err
	}
	defer database.Close()
	var count int
	err = database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name LIKE ? ESCAPE '\'`,
		strings.ReplaceAll(retiredProductTablePrefix, "_", `\_`)+"%").Scan(&count)
	// A database file read without its log can be torn: a server stopped
	// while copying the log into it leaves pages only the log makes whole,
	// which SQLite mends when it opens the folder. Such a file says nothing
	// of whose it is, and the log is read for Hanji's tables alone
	// (2026-11-10: a server killed mid-checkpoint would not start again,
	// "database disk image is malformed").
	if isSQLiteBaseCode(err, sqlite3.SQLITE_CORRUPT, sqlite3.SQLITE_NOTADB) {
		return logHoldsPattern(path+"-wal", retiredProductTableCreation)
	}
	if err != nil || count > 0 {
		return count > 0, err
	}
	return logHoldsPattern(path+"-wal", retiredProductTableCreation)
}

// openDatabaseUnchanged opens a data folder's database file to read it as
// unchanging: nothing is written, no lock is taken.
func openDatabaseUnchanged(path string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+(&url.URL{Path: filepath.ToSlash(path)}).EscapedPath()+"?immutable=1")
}

var retiredProductTableCreation = regexp.MustCompile("(?i)TABLE\\s+[`\"\\[]?" + retiredProductTablePrefix)

// logHoldsPattern says whether a write-ahead log's bytes match a pattern.
func logHoldsPattern(path string, pattern *regexp.Regexp) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	defer file.Close()
	// Read in pieces that overlap by more than a match can span.
	const piece, overlap = 1 << 20, 64
	buffer := make([]byte, piece+overlap)
	kept := 0
	for {
		read, err := io.ReadFull(file, buffer[kept:])
		if pattern.Match(buffer[:kept+read]) {
			return true, nil
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		kept = copy(buffer, buffer[kept+read-overlap:kept+read])
	}
}
