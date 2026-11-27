package app

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The values the version tools write where fetch's differ, as Notion's
// (seen 2026-11-14): a formula's schema entry names its formula and the
// kind of its result — "formula":"prop(\"값\") * 2","resultType":"number" —
// where fetch's names its code by address; a unique ID's value is its
// number alone, "1" where fetch's is "WSEVN-1"; a list is its JSON text
// (mcpVersionColumnValue); and a row's values, a database's rows' and a row
// page's alike, follow its schema's order.

// versionFormula writes a formula's schema entry as the version tools do:
// its formula as written and the kind of its result — number, text,
// checkbox or date (seen 2026-11-14: "prop(\"값\") > 1" checkbox,
// "dateAdd(…)" date, "if(prop(\"체크\"), 1, \"x\")" text, "[1, 2]" number) —
// in place of fetch's codeUrl.
func (source mcpVersionSource) versionFormula(entry map[string]any, property notionMCPDataSourceProperty) {
	delete(entry, "codeUrl")
	entry["formula"] = mcpVersionFormulaText(property)
	entry["resultType"] = source.formulaResultType(property, map[string]bool{})
}

func mcpVersionFormulaText(property notionMCPDataSourceProperty) string {
	expression, _ := property.Config["formula"].(string)
	if strings.TrimSpace(expression) == "" {
		expression, _ = property.Config["expression"].(string)
	}
	return expression
}

// formulaResultType is the kind of a formula's result: what its expression
// gives (notionViewFormulaResult), a list's that of its items, and text for
// one of mixed kinds or unknown.
func (source mcpVersionSource) formulaResultType(property notionMCPDataSourceProperty, visiting map[string]bool) string {
	if kind := notionViewFormulaKnownKind(source.formulaKind(property, visiting)); kind != "" {
		return kind
	}
	return "text"
}

// formulaKind is formulaResultType's kind before one of unknown kind reads as
// text and one that gives nothing but empty() is of no kind
// (notionViewFormulaKind), as a formula naming it takes it.
func (source mcpVersionSource) formulaKind(property notionMCPDataSourceProperty, visiting map[string]bool) string {
	visiting[property.ID] = true
	defer delete(visiting, property.ID)
	kindOf := func(name string) string {
		for _, named := range source.Properties {
			if named.Name != name && named.ID != name {
				continue
			}
			if named.Type == graphValueTypeFormula {
				if visiting[named.ID] {
					return ""
				}
				if kind := source.formulaKind(named, visiting); kind != "" {
					return kind
				}
				return "text"
			}
			switch notionFormulaPropertyType(named.Type) {
			case "number", "date", "text":
				return notionFormulaPropertyType(named.Type)
			case "boolean":
				return "checkbox"
			}
			return ""
		}
		return ""
	}
	expression := mcpVersionFormulaText(property)
	if kind := notionViewFormulaKind(expression, kindOf); kind != "" {
		return kind
	}
	if node, err := parseDatabaseFormula(expression); err == nil && node != nil && node.kind == databaseFormulaNodeList {
		return notionViewFormulaItemResult(node, kindOf, 0)
	}
	return ""
}

// mcpVersionUniqueIDValue is a unique ID's value as the version tools write
// it: its number, without its prefix.
func mcpVersionUniqueIDValue(stored storedNotionDatabaseRowPageProperty) (string, bool) {
	number := strings.TrimSpace(stored.Value.String)
	if !stored.Value.Valid || number == "" || number == "null" {
		return "", false
	}
	return number, true
}

// rowValues are a row's values in its schema's order and then its url, as
// Notion's version tools write a row page's properties and a changed row
// (seen 2026-11-14).
func (source mcpVersionSource) rowValues(values map[string]any, url string) mcpOrderedObject {
	return append(source.valuesInOrder(values), mcpOrderedEntry{Key: "url", Value: url})
}

// mcpVersionRowProperties are a database row page's values at a time, as
// Notion's view-version gives a row page's properties (seen 2026-11-14:
// every value, in its data source's order, then its url); false for a page
// that isn't a database's row, or a row of no database held at the time.
func mcpVersionRowProperties(
	ctx context.Context, app core.App, authority mcpAuthority, pageID string, at time.Time,
) (mcpOrderedObject, bool, error) {
	var properties mcpOrderedObject
	found, err := mcpVersionRowPageAt(ctx, app, authority, pageID, at, func(_ core.App, source mcpVersionSource, reader mcpVersionRowReader) error {
		values, _, err := reader.values(source, pageID, at.UnixMilli())
		properties = source.rowValues(values, mcpPageURL(authority, pageID))
		return err
	})
	return properties, found, err
}

// mcpVersionColumnValue is a value as its SQLite column holds it, as the
// version tools write a row's values (seen 2026-11-22, worker w2369: a
// relation's and a multi-select's "[\"…\"]" in view-version, a database's
// and a row page's, and in get-version-changes, where fetch gives a list): a
// list as its JSON text.
func mcpVersionColumnValue(value any) any {
	if value == nil {
		return nil
	}
	if kind := reflect.TypeOf(value).Kind(); kind != reflect.Slice && kind != reflect.Array {
		return value
	}
	var written bytes.Buffer
	encoder := json.NewEncoder(&written)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(value) != nil {
		return value
	}
	return strings.TrimSuffix(written.String(), "\n")
}
