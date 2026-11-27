//go:build !leafen_devweb

package app

// A release build logs what breaks the schema's rules at the start — a table
// without its class in the instance product export policy or a deletion
// inventory, a foreign key no index serves — and starts as usual
// (schema_migration_export_classes.go, deletion_inventories_start.go,
// foreign_key_child_indexes.go).
const unclassifiedTablesStopStart = false

// A release build logs a native route no key rule names and starts
// (user_key_unclassified.go).
const unclassifiedKeyRoutesStopStart = false
