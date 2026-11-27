package app

import (
	"database/sql"
	"time"
)

// A moved workspace's comment from another tool opens with its author and
// when it was written, on the clock of whoever moves it in (person_clock.go):
// their time zone is kept with the archive's map as it is made, and read back
// as each such comment is written. A map made before it was kept — or one
// with no zone, the person having chosen none — reads the server's.

func keepWorkspaceImportArchiveClock(db *sql.DB, zone string) error {
	_, err := db.Exec(`CREATE TABLE clock (zone TEXT NOT NULL); INSERT INTO clock (zone) VALUES (?)`, zone)
	return err
}

func workspaceImportArchiveClock(db *sql.DB) *time.Location {
	var zone string
	if db.QueryRow(`SELECT zone FROM clock LIMIT 1`).Scan(&zone) != nil {
		zone = ""
	}
	return personZoneLocation(zone)
}
