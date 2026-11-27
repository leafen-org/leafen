package app

import (
	"strconv"
	"strings"
)

// A page's or a database's cover or image icon names a managed file in one
// of two forms: the file's own ID, as the API and imports keep it, or the
// file's address under the API, as the screen keeps an uploaded one. Every
// owner that records, protects, copies, exports or serves such a file reads
// the value here, so both forms name the same file.

const productFilePresentationPrefix = apiRoot + "/files/"

// productFilePresentationID is the managed file a cover or image icon shows,
// or false when the value is not one (a gallery cover, an emoji, a link).
func productFilePresentationID(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if fileID, found := strings.CutPrefix(value, productFilePresentationPrefix); found {
		value = fileID
	}
	return value, workspaceExportFileRecordIDPattern.MatchString(value)
}

// productFilePresentationWithID is the value showing another file in the same
// form, as a copy of a page writes its copied cover.
func productFilePresentationWithID(value, fileID string) string {
	if strings.HasPrefix(strings.TrimSpace(value), productFilePresentationPrefix) {
		return productFilePresentationPrefix + fileID
	}
	return fileID
}

// productFilePresentationSQL reads a column holding a cover or icon as its
// file ID, for queries that join it to the file it shows.
func productFilePresentationSQL(column string) string {
	length := strconv.Itoa(len(productFilePresentationPrefix))
	return "CASE WHEN substr(" + column + ",1," + length + ")='" + productFilePresentationPrefix + "' THEN substr(" +
		column + "," + strconv.Itoa(len(productFilePresentationPrefix)+1) + ") ELSE " + column + " END"
}
