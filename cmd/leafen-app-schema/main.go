// Command leafen-app-schema writes the app SDK's manifest JSON Schema and
// TypeScript types from the manifest's Go structure, or checks them:
//
//	go run ./cmd/leafen-app-schema [--check]
//
// Run it after changing the manifest (internal/app/app_platform_manifest.go).
package main

import (
	"bytes"
	"flag"
	"log"
	"os"

	"leafen.org/leafen/internal/app"
)

func main() {
	check := flag.Bool("check", false, "fail when the files differ instead of writing them")
	flag.Parse()
	schema, types, err := app.AppManifestSchemaFiles()
	if err != nil {
		log.Fatal(err)
	}
	stale := false
	for path, data := range map[string][]byte{"sdk/app/schema/leafen-app.schema.json": schema, "sdk/app/src/manifest.ts": types} {
		if *check {
			current, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(current, data) {
				log.Printf("%s is stale; run go run ./cmd/leafen-app-schema", path)
				stale = true
			}
			continue
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			log.Fatal(err)
		}
	}
	if stale {
		os.Exit(1)
	}
}
