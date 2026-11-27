package app

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
)

type storedFileTypeAdmissionError struct {
	code string
}

func (failure *storedFileTypeAdmissionError) Error() string {
	return failure.code
}

func admitStoredFileType(name string, contentType string) (string, error) {
	normalized, err := normalizeStoredFileContentType(contentType)
	if err != nil {
		return "", err
	}
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.TrimSpace(name))), ".")
	// A build with the app platform keeps an HTML file, named and typed as
	// one, to open only in a frame with no origin (docs/app-platform.md 14);
	// it is never served inline from Leafen's own origin.
	if _, html := sandboxedHTMLExtensions[extension]; html && appPlatformAvailable &&
		(normalized == sandboxedHTMLContentType || normalized == defaultFileContentType) {
		return sandboxedHTMLContentType, nil
	}
	if _, blocked := activeFileContentTypes[normalized]; blocked || strings.HasSuffix(normalized, "+xml") {
		return "", &storedFileTypeAdmissionError{code: errorCodeFileUnsafeType}
	}
	if _, blocked := activeFileExtensions[extension]; blocked {
		return "", &storedFileTypeAdmissionError{code: errorCodeFileUnsafeType}
	}
	return normalized, nil
}

// sandboxStoredFileDownload keeps a stored HTML file, read straight from
// Leafen's origin, unable to run there: the file server already sends it as
// an attachment, and should a browser open it anyway it has no origin and
// no scripts. An SVG a Notion migration kept, cleaned already
// (notion_import_svg.go), opens the same way, its own styles and held
// pictures drawn.
func sandboxStoredFileDownload(header http.Header, contentType string) {
	base, _, _ := strings.Cut(contentType, ";")
	switch strings.ToLower(strings.TrimSpace(base)) {
	case sandboxedHTMLContentType:
		header.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	case notionImportSVGContentType:
		header.Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src data:")
	}
}

func normalizeStoredFileContentType(contentType string) (string, error) {
	base, _, _ := strings.Cut(contentType, ";")
	normalized := strings.ToLower(strings.TrimSpace(base))
	if normalized == "" {
		normalized = defaultFileContentType
	}
	if len(normalized) > maxFileContentTypeLength || !validStoredFileContentType(normalized) {
		return "", &storedFileTypeAdmissionError{code: errorCodeFileContentTypeInvalid}
	}
	return normalized, nil
}

func validStoredFileContentType(value string) bool {
	major, minor, found := strings.Cut(value, "/")
	return found && validStoredFileContentTypeToken(major) && validStoredFileContentTypeToken(minor)
}

func validStoredFileContentTypeToken(value string) bool {
	if value == "" {
		return false
	}
	for index, current := range value {
		if current >= 'a' && current <= 'z' || current >= '0' && current <= '9' {
			continue
		}
		if index > 0 && (current == '.' || current == '+' || current == '-') {
			continue
		}
		return false
	}
	return true
}

func storedFileTypeAdmissionCode(err error) string {
	var failure *storedFileTypeAdmissionError
	if errors.As(err, &failure) {
		return failure.code
	}
	return ""
}
