package app

import (
	"encoding/json"
	"path"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
)

// fileTextTypes and fileTextExtensions are the files whose words are read,
// and the reader each takes, by type or else by name.
var (
	fileTextTypes = map[string]string{
		"application/pdf": "pdf",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   "docx",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         "xlsx",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation": "pptx",
		"application/hwp+zip": "hwpx", "application/vnd.hancom.hwpx": "hwpx", "application/haansofthwpx": "hwpx",
		"application/x-hwp": "hwp", "application/haansofthwp": "hwp", "application/vnd.hancom.hwp": "hwp",
		"application/vnd.oasis.opendocument.text":         "odt",
		"application/vnd.oasis.opendocument.spreadsheet":  "ods",
		"application/vnd.oasis.opendocument.presentation": "odp",
		"application/msword":                              "doc", "application/vnd.ms-excel": "xls", "application/vnd.ms-powerpoint": "ppt",
	}
	fileTextExtensions = []string{".pdf", ".docx", ".xlsx", ".pptx", ".hwp", ".hwpx", ".odt", ".ods", ".odp", ".doc", ".xls", ".ppt"}
)

// fileTextKind is which reader a file takes, by its type or else its name.
func fileTextKind(contentType, name string) string {
	if kind, known := fileTextTypes[strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))]; known {
		return kind
	}
	extension := strings.ToLower(path.Ext(name))
	for _, known := range fileTextExtensions {
		if extension == known {
			return extension[1:]
		}
	}
	return ""
}

// fileTextCandidateSQL picks the files fileTextKind gives a reader.
func fileTextCandidateSQL(file string, params dbx.Params) string {
	types, _ := json.Marshal(func() []string {
		names := []string{}
		for name := range fileTextTypes {
			names = append(names, name)
		}
		sort.Strings(names)
		return names
	}())
	extensions, _ := json.Marshal(fileTextExtensions)
	params["fileTextTypes"], params["fileTextExtensions"] = string(types), string(extensions)
	return `(` + file + `.[[content_type]] IN (SELECT [[value]] FROM json_each({:fileTextTypes}))
		OR EXISTS (SELECT 1 FROM json_each({:fileTextExtensions}) [[extension]]
			WHERE lower(` + file + `.[[original_name]]) LIKE '%' || [[extension]].[[value]]))`
}
