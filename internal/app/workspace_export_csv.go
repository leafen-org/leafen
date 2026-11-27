package app

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// writeWorkspaceExportDatabaseCSV writes a database alone as the CSV a
// Markdown & CSV export holds beside it: its "_all" file, every property and
// every row, values as the database shows them, as Notion's CSV writes them
// (2026-11-12: this file alone wrote the stored values — a status's "done",
// a date's {"start":…}, a select's "option-0").
func writeWorkspaceExportDatabaseCSV(app core.App, output io.Writer, job storedJob, payload workspaceExportJobPayload) error {
	document, err := collectWorkspaceExportDocument(app, job, payload)
	if err != nil {
		return err
	}
	text, err := document.databaseCSV(payload.Scope.TargetID, true)
	if err != nil {
		return err
	}
	_, err = io.WriteString(output, text)
	return err
}

func workspaceExportCSVValue(raw any) string {
	value := raw
	if wrapper, ok := raw.(map[string]any); ok {
		value = wrapper["value"]
	}
	var text string
	switch typed := value.(type) {
	case nil:
		text = ""
	case string:
		text = typed
	case float64, bool:
		text = fmt.Sprint(typed)
	default:
		text = workspaceExportJSONText(typed)
	}
	return csvText(text)
}

// csvNumber is a whole cell a spreadsheet reads as a number: "-1.5", "+3",
// "-0.25e3".
var csvNumber = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

// csvText is a CSV cell as Leafen's CSV files write it (exports, the members
// file): a value a spreadsheet would run as a formula — one starting with
// "=", "+", "-", "@", a tab or a carriage return that isn't a number — is
// kept as text by a "'" before it, and a number, negative or signed, is
// written as it is, as Notion's export writes it (seen 2026-11-12: "-1.5").
// Notion writes the others bare too ("@2026년 1월 2일 오전 10:30,…").
func csvText(text string) string {
	if text != "" && strings.ContainsRune("=+-@\t\r", rune(text[0])) && !csvNumber.MatchString(text) {
		return "'" + text
	}
	return text
}
