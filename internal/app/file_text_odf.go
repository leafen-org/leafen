package app

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"strings"
)

// The words of OpenDocument files — LibreOffice's text, spreadsheet and
// presentation — reach search as those of Office files do
// (file_text_extraction.go): each is a zip whose content.xml holds the
// paragraphs and headings of the text, the cells of the sheets and the text
// of the slides, each a paragraph, and a spreadsheet's sheet names, as an
// xlsx's are read. A file locked by a password keeps its words.

// odfText reads an OpenDocument file's content, a paragraph a line.
func odfText(archive *zip.Reader, kind string, collected *fileTextCollector) error {
	var content *zip.File
	for _, file := range archive.File {
		switch file.Name {
		case "content.xml":
			content = file
		case "META-INF/manifest.xml":
			if locked, err := odfEncrypted(file); err != nil || locked {
				return err
			}
		}
	}
	if content == nil {
		return nil
	}
	reader, err := content.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	decoder := xml.NewDecoder(io.LimitReader(reader, fileTextMaxPartBytes))
	decoder.Strict = false
	line := strings.Builder{}
	depth := 0
	for !collected.full() {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "table":
				for _, attribute := range element.Attr {
					if kind == "ods" && attribute.Name.Local == "name" {
						collected.add(attribute.Value)
					}
				}
			case "p", "h":
				depth++
			// Spaces, tabs and line breaks are elements of their own.
			case "s", "tab":
				line.WriteByte(' ')
			case "line-break":
				line.WriteByte('\n')
			}
		case xml.EndElement:
			if (element.Name.Local == "p" || element.Name.Local == "h") && depth > 0 {
				depth--
				if depth == 0 {
					collected.add(line.String())
					line.Reset()
				}
			}
		case xml.CharData:
			if depth > 0 {
				line.Write(element)
			}
		}
	}
	collected.add(line.String())
	return nil
}

// odfEncrypted is whether an OpenDocument file's manifest says its parts are
// encrypted.
func odfEncrypted(manifest *zip.File) (bool, error) {
	reader, err := manifest.Open()
	if err != nil {
		return false, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, fileTextMaxPartBytes))
	return strings.Contains(string(data), "encryption-data"), err
}
