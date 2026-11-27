//go:build leafen_devweb

package app

// A development build stops where the schema breaks its rules at the start:
// a table without its class in the instance product export policy or a
// deletion inventory, a foreign key no index serves
// (schema_migration_export_classes.go, deletion_inventories_start.go,
// foreign_key_child_indexes.go).
const unclassifiedTablesStopStart = true

// A development build stops at the start, too, on a native route no key rule
// names (user_key_unclassified.go).
const unclassifiedKeyRoutesStopStart = true
