package app

import (
	"sort"
	"strings"
)

// The blocks that carry a stored file are graphManagedFileBlockTypes, the one
// boundary every place that keeps or lists a file for its blocks reads: live
// blocks, block trash, page history and what the file cleanup counts as a
// reference (2026-11-06: lists written out by hand had drifted, so a PDF or an
// embed whose file only trash or history still held could lose that file).
// managedFileBlockTypesSQL is that set as an SQL list. A type added there
// needs a migration rewriting page history's stored trigger, as migration 700
// does (schema_baseline.json).
var managedFileBlockTypesSQL = func() string {
	types := make([]string, 0, len(graphManagedFileBlockTypes))
	for blockType := range graphManagedFileBlockTypes {
		types = append(types, "'"+blockType+"'")
	}
	sort.Strings(types)
	return "IN (" + strings.Join(types, ",") + ")"
}()
