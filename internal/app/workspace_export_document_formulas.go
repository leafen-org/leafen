package app

import (
	"encoding/json"
	"strings"
)

// A formula's value is written as Notion's export writes it (seen 2026-11-12,
// Markdown & CSV and HTML of the same database, a Seoul exporter in Korean):
//
//   - a date as format() writes it, on the clock it keeps, naming that clock
//     only when its offset is not the exporter's, "2026년 1월 2일 오전 10:30",
//     "2026년 5월 6일 오전 8:00 (UTC)", a range "… → …", with no "@" and in
//     HTML no <time>;
//   - true and false as "Yes" and "No", in HTML as a checked or unchecked
//     disabled checkbox;
//   - a person or a page by its name;
//   - a list's items between commas, "가,나", "1,2.5", "true,false", each
//     date after an "@", "@2026년 1월 2일 오전 10:30,@2026년 1월 3일 오전
//     10:30", and people and pages between spaces, "〈봇〉 〈사람〉".
//
// Numbers and text read as the app's cell shows them.

// formulaCellPart writes a formula's computed value.
func (document *workspaceExportDocument) formulaCellPart(raw json.RawMessage) workspaceExportCellPart {
	value := databaseFormulaInput(databaseQueryRawValue(raw))
	if checked, ok := value.(bool); ok {
		return workspaceExportCheckboxPart(checked)
	}
	return workspaceExportCellPart{Text: document.formulaValueText(value, true)}
}

func (document *workspaceExportDocument) formulaValueText(value any, top bool) string {
	switch typed := value.(type) {
	case databaseFormulaDate:
		text := document.formulaEnvironment().dateText(typed)
		if !top {
			return "@" + text
		}
		return text
	case databaseFormulaRichText:
		return typed.shown()
	case databaseFormulaReference:
		return typed.Name
	case []any:
		separator := ","
		parts := make([]string, 0, len(typed))
		references := len(typed) > 0
		for _, item := range typed {
			_, reference := item.(databaseFormulaReference)
			references = references && reference
			parts = append(parts, document.formulaValueText(item, false))
		}
		if references {
			separator = " "
		}
		return strings.Join(parts, separator)
	}
	return workspaceExportComputedValueText(value)
}

// workspaceExportCheckboxPart is a checkbox's value: "Yes" or "No", in HTML
// a disabled checkbox, as Notion's export writes a checkbox property and a
// true or false formula (seen 2026-11-14 in the file: <input type="checkbox"
// class="checkbox checkbox-on" disabled="" checked=""/>, a to-do's alike).
func workspaceExportCheckboxPart(checked bool) workspaceExportCellPart {
	part := workspaceExportCellPart{Text: "No", Checkbox: "off"}
	if checked {
		part.Text, part.Checkbox = "Yes", "on"
	}
	return part
}
