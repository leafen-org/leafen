package app

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"html"
	"io"
	"net/url"
	"path"
	"strings"
)

// An SVG file a Notion migration brings is kept with what could run or reach
// out taken from it, where Leafen refuses any other upload of one
// (file-security-policy.json, active types): a company's logo is a file a
// person means to keep (2026-11-16, a real migration left two out). What is
// kept is the drawing — shapes, text, gradients, styles and pictures held in
// the file — without scripts, event handlers, embedded documents or links
// and references to anything outside the file. It is served under a sandbox
// (sandboxStoredFileDownload), and an image block draws it as an image, which
// never runs a script. The scan names each one beforehand.

// notionImportSVGContentType is what a kept SVG is stored as.
const notionImportSVGContentType = "image/svg+xml"

// maxNotionImportSVGSize bounds an SVG read whole to be cleaned; a larger one
// is left out as before.
const maxNotionImportSVGSize = 16 << 20

// notionImportSVGFile is a file Notion names or types as an SVG drawing (not
// a compressed one, which is left out as before).
func notionImportSVGFile(name, contentType string) bool {
	base, _, _ := strings.Cut(contentType, ";")
	base = strings.ToLower(strings.TrimSpace(base))
	return base == notionImportSVGContentType ||
		strings.EqualFold(path.Ext(strings.TrimSpace(name)), ".svg") && (base == "" || base == defaultFileContentType || base == "text/xml" || base == "application/xml")
}

// readNotionImportSVG reads an SVG whole and gives its cleaned drawing; one too
// large or not an SVG drawing is left out as Leafen's file policy leaves it.
func readNotionImportSVG(body io.Reader) (io.Reader, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxNotionImportSVGSize+1))
	if err != nil {
		return nil, &notionFileFetchFailure{code: errorCodeNotionImportFileTransient, kind: "transient"}
	}
	if len(data) > maxNotionImportSVGSize {
		return nil, &notionFileFetchFailure{code: errorCodeFileUnsafeType, kind: "permanent"}
	}
	cleaned, err := cleanNotionImportSVG(data)
	if err != nil {
		return nil, &notionFileFetchFailure{code: errorCodeFileUnsafeType, kind: "permanent"}
	}
	return bytes.NewReader(cleaned), nil
}

// notionImportSVGElementsLeftOut are the elements taken out whole, with all
// they hold: what runs, embeds another document, or changes a link as it
// plays.
var notionImportSVGElementsLeftOut = map[string]bool{
	"script": true, "foreignobject": true, "iframe": true, "object": true, "embed": true,
	"handler": true, "listener": true, "audio": true, "video": true, "set": true,
}

// cleanNotionImportSVG writes the drawing of an SVG again without what could
// run or reach out. It refuses a file that isn't an SVG drawing.
func cleanNotionImportSVG(data []byte) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity
	var out bytes.Buffer
	skipping, depth, root := 0, 0, false
	for {
		token, err := decoder.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch current := token.(type) {
		case xml.StartElement:
			depth++
			local := strings.ToLower(current.Name.Local)
			if !root {
				if local != "svg" {
					return nil, errors.New(errorCodeFileUnsafeType)
				}
				root = true
			}
			if skipping > 0 || notionImportSVGElementsLeftOut[local] || strings.HasPrefix(local, "animate") && notionImportSVGAnimatesLink(current) {
				skipping++
				continue
			}
			out.WriteString("<" + notionImportSVGName(current.Name))
			for _, attribute := range current.Attr {
				if value, keep := notionImportSVGAttribute(attribute); keep {
					out.WriteString(" " + notionImportSVGName(attribute.Name) + `="` + html.EscapeString(value) + `"`)
				}
			}
			out.WriteString(">")
		case xml.EndElement:
			depth--
			if skipping > 0 {
				skipping--
				continue
			}
			out.WriteString("</" + notionImportSVGName(current.Name) + ">")
		case xml.CharData:
			if skipping == 0 && depth > 0 {
				out.WriteString(html.EscapeString(notionImportSVGStyleText(string(current))))
			}
		}
		// Comments, processing instructions and declarations (a DOCTYPE's
		// entities among them) are left out.
	}
	if !root || depth != 0 {
		return nil, errors.New(errorCodeFileUnsafeType)
	}
	return out.Bytes(), nil
}

func notionImportSVGName(name xml.Name) string {
	if name.Space == "" {
		return name.Local
	}
	return name.Space + ":" + name.Local
}

// notionImportSVGAttribute keeps an attribute unless it handles an event, is
// a link or reference out of the file, or names a script.
func notionImportSVGAttribute(attribute xml.Attr) (string, bool) {
	local := strings.ToLower(attribute.Name.Local)
	value := attribute.Value
	squeezed := strings.ToLower(strings.Join(strings.Fields(value), ""))
	switch {
	case strings.HasPrefix(local, "on"):
		return "", false
	case strings.Contains(squeezed, "javascript:") || strings.Contains(squeezed, "vbscript:"):
		return "", false
	case local == "href" || local == "src" || local == "base" && attribute.Name.Space == "xml":
		// A reference within the file, or a picture held in it.
		return value, strings.HasPrefix(strings.TrimSpace(value), "#") || notionImportSVGHeldPicture(squeezed)
	case local == "style":
		return notionImportSVGStyleText(value), true
	}
	if strings.Contains(squeezed, "url(") {
		return notionImportSVGStyleText(value), true
	}
	return value, true
}

// notionImportSVGHeldPicture is a raster picture written into the file.
func notionImportSVGHeldPicture(value string) bool {
	for _, kind := range []string{"png", "jpeg", "jpg", "gif", "webp"} {
		if strings.HasPrefix(value, "data:image/"+kind+";") || strings.HasPrefix(value, "data:image/"+kind+",") {
			return true
		}
	}
	return false
}

// notionImportSVGAnimatesLink is an animation that changes a link as it plays.
func notionImportSVGAnimatesLink(element xml.StartElement) bool {
	for _, attribute := range element.Attr {
		if strings.EqualFold(attribute.Name.Local, "attributeName") && strings.Contains(strings.ToLower(attribute.Value), "href") {
			return true
		}
	}
	return false
}

// notionImportSVGStyleText takes out of styles what reaches outside the file:
// an @import, and a url() not naming a part of it.
func notionImportSVGStyleText(text string) string {
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "url(") && !strings.Contains(lower, "@import") {
		return text
	}
	var out strings.Builder
	for index := 0; index < len(text); {
		rest := strings.ToLower(text[index:])
		switch {
		case strings.HasPrefix(rest, "@import"):
			end := strings.IndexByte(text[index:], ';')
			if end < 0 {
				return out.String()
			}
			index += end + 1
		case strings.HasPrefix(rest, "url("):
			end := strings.IndexByte(text[index:], ')')
			if end < 0 {
				return out.String()
			}
			inner := strings.Trim(strings.TrimSpace(text[index+4:index+end]), `"'`)
			if strings.HasPrefix(inner, "#") || notionImportSVGHeldPicture(strings.ToLower(strings.Join(strings.Fields(inner), ""))) {
				out.WriteString(text[index : index+end+1])
			} else {
				out.WriteString("none")
			}
			index += end + 1
		default:
			out.WriteByte(text[index])
			index++
		}
	}
	return out.String()
}

// notionMigrationSVGIssue names, in the scan, an SVG file and its page, so a
// person knows before the import it comes cleaned.
func notionMigrationSVGIssue(change *notionMigrationScanChange, pageID string, block notionProviderObject) {
	if !notionImportFileSourceKind(block.Type) || len(block.Payload) == 0 {
		return
	}
	fields := struct {
		Name string `json:"name"`
		File struct {
			URL string `json:"url"`
		} `json:"file"`
	}{}
	if json.Unmarshal(block.Payload, &fields) != nil {
		return
	}
	name := strings.TrimSpace(fields.Name)
	if name == "" {
		if parsed, err := url.Parse(fields.File.URL); err == nil {
			name = path.Base(parsed.Path)
		}
	}
	if !strings.EqualFold(path.Ext(name), ".svg") {
		return
	}
	change.issues = append(change.issues, notionMigrationIssue{Code: notionImportIssueSVGCleaned, SourceID: pageID, Detail: name})
}

func init() {
	notionMigrationReportText[accountLanguageKo]["issue:"+notionImportIssueSVGCleaned] = "스크립트·외부 연결을 빼고 옮긴 SVG 파일 %d개"
	notionMigrationReportText[accountLanguageEn]["issue:"+notionImportIssueSVGCleaned] = "%d SVG files brought without their scripts and outside links"
}
