package app

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
)

// productMigrationImpact is build-time ownership metadata kept beside the
// ordered runtime migration that creates the impact. The private verification
// projection reads these stable codes directly from this product-owned ledger.
type productMigrationImpact struct {
	responsibilityCodes string
	policyConsumers     string
}

func schemaMigrationImpact(responsibilityCodes, policyConsumers string) productMigrationImpact {
	return productMigrationImpact{
		responsibilityCodes: responsibilityCodes,
		policyConsumers:     policyConsumers,
	}
}

// A migration's up changes the schema with SQL written for its own version,
// and runs in version order. Work that goes through today's product code
// (graph operations, document loads, search marks) is its content instead:
// that code reads the schema as the last migration leaves it, so on an
// upgrade every content step runs once all ups have, in version order, in
// the same transaction (2026-11-11: an install at 714 couldn't start, as
// 715 read a column 719 adds). Work that must read storage a later
// migration retires stays in up, with its own SQL.
type productMigration struct {
	version  int
	name     string
	checksum string
	impact   productMigrationImpact
	up       func(core.App) error
	content  func(core.App) error
	validate func(core.App) error
}

// productMigrations is every migration in version order. The first is the
// schema baseline (schema_baseline.go): the schema migrations 1 to 831 left,
// in one step a new database takes and records as version 831. Data that
// kept the history of those migrations one by one is not opened
// (schema_history_refusal.go). The migrations after it are those of
// schema_migration_definitions_0832.go, where a new migration goes at the
// end; scripts/generate-schema-migration-impact.mjs reads them all in that
// order.
var productMigrations = slices.Concat([]productMigration{
	{version: 831, name: "native_schema_baseline", checksum: schemaBaselineChecksum, impact: schemaMigrationImpact("BPR-002,BPR-003,BPR-004,BPR-005,BPR-006,BPR-007,BPR-008,BPR-009,BPR-010,BPR-011,BPR-012,BPR-013,BPR-014,BPR-015,BPR-016,BPR-017,BPR-018,BPR-019,BPR-020,BPR-021,BPR-022,BPR-023,BPR-024,BPR-025,BPR-026,BPR-027,BPR-028,BPR-029,BPR-034,BPR-035,BPR-036,BPR-037,BPR-038,BPR-039,BPR-040,BPR-043", "instance-product-export"), up: applySchemaBaseline, validate: validateSchemaBaseline},
}, productMigrationsFrom0832)
