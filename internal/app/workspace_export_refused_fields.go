package app

import (
	"maps"
	"reflect"
	"slices"
	"strings"
)

// An export refused for what its format doesn't take says which part, and
// what that format takes (2026-11-26, real agents: "Check the export
// details." answered a workspace's id as scope.targetId and includeComments
// with Markdown & CSV alike, and every try was refused). What each format
// takes is asked of the same checks the request went through
// (normalizeWorkspaceExportScope, normalizeWorkspaceExportOptions), so it
// can't say other than they do.

// workspaceExportRefusedFields are the parts of an export's request its
// format doesn't take: the scope's kind or target, and its options.
func workspaceExportRefusedFields(format string, scope workspaceExportScope, options map[string]bool) []apiErrorField {
	fields := []apiErrorField{}
	kinds := []string{}
	for _, kind := range []string{workspaceExportScopeWorkspace, workspaceExportScopePage, workspaceExportScopeDatabase, workspaceExportScopeBlockSubtree} {
		target := "id"
		if kind == workspaceExportScopeWorkspace {
			target = ""
		}
		if _, takes := normalizeWorkspaceExportScope(format, workspaceExportScope{Kind: kind, TargetID: target}); takes {
			kinds = append(kinds, kind)
		}
	}
	// A format no kind takes isn't one.
	if len(kinds) == 0 {
		formats := []string{}
		for _, known := range workspaceExportFormats {
			for _, kind := range []string{workspaceExportScopeWorkspace, workspaceExportScopePage, workspaceExportScopeDatabase, workspaceExportScopeBlockSubtree} {
				if _, takes := normalizeWorkspaceExportScope(known, workspaceExportScope{Kind: kind, TargetID: "id"}); takes && !slices.Contains(formats, known) {
					formats = append(formats, known)
				}
				if _, takes := normalizeWorkspaceExportScope(known, workspaceExportScope{Kind: kind}); takes && !slices.Contains(formats, known) {
					formats = append(formats, known)
				}
			}
		}
		return append(fields, apiErrorField{Name: "format", Problem: apiErrorFieldNotOneOf, Allowed: formats})
	}
	if _, valid := normalizeWorkspaceExportScope(format, scope); !valid {
		kind := strings.TrimSpace(scope.Kind)
		switch {
		case !slices.Contains(kinds, kind):
			fields = append(fields, apiErrorField{Name: "scope.kind", Problem: "not taken with format " + format, Allowed: kinds})
		case kind == workspaceExportScopeWorkspace:
			fields = append(fields, apiErrorField{Name: "scope.targetId", Problem: "not taken with kind workspace: the path names the workspace"})
		default:
			fields = append(fields, apiErrorField{Name: "scope.targetId", Problem: apiErrorFieldMissing})
		}
	}
	if _, valid := normalizeWorkspaceExportOptions(format, options); valid {
		return fields
	}
	takes := []string{}
	for _, key := range workspaceExportOptionNames() {
		if _, valid := normalizeWorkspaceExportOptions(format, map[string]bool{"includeFiles": true, key: key == "includeFiles"}); valid {
			takes = append(takes, key)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(options)) {
		if !slices.Contains(takes, key) {
			fields = append(fields, apiErrorField{Name: "options." + key, Problem: "not taken with format " + format, Allowed: takes})
		}
	}
	if include, given := options["includeFiles"]; !given {
		fields = append(fields, apiErrorField{Name: "options.includeFiles", Problem: apiErrorFieldMissing})
	} else if _, valid := normalizeWorkspaceExportOptions(format, map[string]bool{"includeFiles": include}); !valid {
		fields = append(fields, apiErrorField{Name: "options.includeFiles", Problem: "must be true with format " + format})
	}
	return fields
}

// workspaceExportFormats are the formats an export's request may name.
var workspaceExportFormats = []string{
	workspaceExportFormatMarkdownCSV, workspaceExportFormatHTML, workspaceExportFormatPrintHTML, workspaceExportFormatNativeArchive,
	workspaceExportFormatPageMarkdown, workspaceExportFormatNotionMarkdown, workspaceExportFormatDatabaseCSV, workspaceExportFormatPageNativeArchive,
}

// workspaceExportOptionNames are the options an export's request may name,
// as the job keeps them.
func workspaceExportOptionNames() []string {
	names := []string{}
	kind := reflect.TypeFor[workspaceExportOptions]()
	for index := range kind.NumField() {
		if name, _, _ := strings.Cut(kind.Field(index).Tag.Get("json"), ","); name != "" && name != "-" {
			names = append(names, name)
		}
	}
	return names
}
