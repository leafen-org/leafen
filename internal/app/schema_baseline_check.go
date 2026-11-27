package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/pocketbase/pocketbase"
)

// CheckSchemaBaseline does in a scratch folder what a new data folder's
// first start does with the schema: a fresh PocketBase database takes the
// baseline and the migrations after it, and passes their checks, the export
// classes, the deletion inventories and the foreign key indexes as a
// development build's start holds them. It says what it applied.
func CheckSchemaBaseline(scratchDir string) (string, error) {
	pb, err := schemaBaselineScratchApp(filepath.Join(scratchDir, "check"))
	if err != nil {
		return "", err
	}
	defer func() { _ = pb.ClearBootstrap() }()
	if err := runProductMigrations(pb); err != nil {
		return "", err
	}
	tables, err := schemaTableNames(pb)
	if err != nil {
		return "", err
	}
	if missing := instanceProductExportUnclassified(instanceProductExportPolicyDefinitions(), tables); len(missing) > 0 {
		return "", fmt.Errorf("tables without a class in instance_product_export_policy.go: %v", missing)
	}
	for _, inventory := range deletionInventories {
		if err := inventory.check(pb); err != nil {
			return "", fmt.Errorf("%s: %w", inventory.message, err)
		}
	}
	if err := validateForeignKeyChildIndexes(pb); err != nil {
		return "", err
	}
	objects, err := readSchemaBaselineObjects()
	if err != nil {
		return "", err
	}
	summary := fmt.Sprintf("schema baseline %d applies: %d objects", schemaBaselineVersion, len(objects))
	if last := productMigrations[len(productMigrations)-1].version; last > schemaBaselineVersion {
		summary += fmt.Sprintf(", then migrations %d to %d", schemaBaselineVersion+1, last)
	}
	return summary, nil
}

// RecordSchemaBaseline records again what of PocketBase's own the baseline
// replaces, as this build's PocketBase makes it, into the baseline it is
// given, and checks the result applies. It is for after the baseline was
// updated for a PocketBase that changed its users table or collection: the
// baseline's own statements say how the schema becomes Leafen's, and the
// record says what they were written over.
func RecordSchemaBaseline(scratchDir string, source []byte) ([]byte, error) {
	baseline := schemaBaseline{}
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()
	if err := decoder.Decode(&baseline); err != nil {
		return nil, err
	}
	if baseline.Version != schemaBaselineVersion || baseline.Migrations != schemaBaselineChecksum {
		return nil, errors.New("this is not the baseline this build names")
	}
	pb, err := schemaBaselineScratchApp(filepath.Join(scratchDir, "record"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = pb.ClearBootstrap() }()
	if baseline.Replaced, err = schemaBaselineReplacedNow(pb, baseline); err != nil {
		return nil, err
	}
	baseline.Format = schemaBaselineFormat
	encoded, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// schemaBaselineScratchApp is a fresh PocketBase database in a folder of
// its own, as a new data folder's start has it before the migrations.
func schemaBaselineScratchApp(dir string) (*pocketbase.PocketBase, error) {
	pb := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: dir, HideStartBanner: true})
	if err := pb.Bootstrap(); err != nil {
		return nil, err
	}
	return pb, nil
}
