// Command leafen-schema-baseline checks the schema baseline a new data
// folder takes in one step (internal/app/schema_baseline.go): that this
// build's PocketBase makes what the baseline was recorded over, and that a
// fresh database takes the baseline and the migrations after it and passes
// their checks:
//
//	go run ./cmd/leafen-schema-baseline
//
// A new migration needs nothing from it: the baseline stays as it is. After a
// PocketBase update that changes its users table or collection, a new data
// folder's start stops; update the baseline's statements for it, then record
// what they are written over again and check it:
//
//	go run ./cmd/leafen-schema-baseline -write
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"leafen.org/leafen/internal/app"
)

func main() {
	path := flag.String("baseline", "internal/app/schema_baseline.json", "the baseline file")
	write := flag.Bool("write", false, "record again what of PocketBase's own the baseline replaces")
	flag.Parse()
	scratch, err := os.MkdirTemp("", "leafen-schema-baseline-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(scratch)
	if *write {
		source, err := os.ReadFile(*path)
		if err != nil {
			log.Fatal(err)
		}
		encoded, err := app.RecordSchemaBaseline(scratch, source)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(*path, encoded, 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("recorded %s; build again and check it\n", *path)
		return
	}
	summary, err := app.CheckSchemaBaseline(scratch)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(summary)
}
