package app

import (
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/encoding/charmap"
)

// The words of Office 97–2003 files reach search as those of newer ones do
// (file_text_extraction.go). Each is a compound file, as an HWP 5 file is
// (file_text_hwp.go): a Word document keeps its text in pieces its table
// stream lists, a PowerPoint presentation in text records among its slides'
// records, an Excel workbook in a table of shared strings and in its cells
// and sheet names. A file locked by a password keeps its words; one older
// than Office 97 is not read. Everything a file says about lengths and
// places is checked against what it holds, as it may be broken or made to
// mislead.

func legacyOfficeText(kind string, source io.ReaderAt, size int64, collected *fileTextCollector) error {
	file, err := openCompoundFile(source, size)
	if err != nil {
		return err
	}
	switch kind {
	case "doc":
		return wordText(file, collected)
	case "xls":
		return excelText(file, collected)
	default:
		return powerPointText(file, collected)
	}
}

// legacyLines gathers decoded characters into lines for the collector.
type legacyLines struct {
	line      strings.Builder
	collected *fileTextCollector
}

func (lines *legacyLines) put(character rune) {
	if character == '\n' {
		lines.flush()
		return
	}
	lines.line.WriteRune(character)
	if lines.line.Len() > 1<<16 {
		lines.flush()
	}
}

func (lines *legacyLines) flush() {
	lines.collected.add(lines.line.String())
	lines.line.Reset()
}

const (
	wordIdentifier   = 0xa5ec
	wordFirstFib97   = 0x00c1
	wordEncrypted    = 0x0100
	wordTableOne     = 0x0200
	wordPieceSqueeze = 0x40000000
)

// wordText reads a Word 97–2003 document's pieces of text in their order,
// leaving out field codes, keeping what the fields show.
func wordText(file *compoundFile, collected *fileTextCollector) error {
	document, err := file.stream("WordDocument")
	if err != nil {
		return err
	}
	if len(document) < 0x1aa || binary.LittleEndian.Uint16(document) != wordIdentifier {
		return errors.New("not a Word document")
	}
	flags := binary.LittleEndian.Uint16(document[0x0a:])
	if flags&wordEncrypted != 0 {
		return nil
	}
	if binary.LittleEndian.Uint16(document[2:]) < wordFirstFib97 {
		return errors.New("a Word document older than Word 97")
	}
	tableName := "0Table"
	if flags&wordTableOne != 0 {
		tableName = "1Table"
	}
	table, err := file.stream(tableName)
	if err != nil {
		return err
	}
	clxAt, clxLength := uint64(binary.LittleEndian.Uint32(document[0x1a2:])), uint64(binary.LittleEndian.Uint32(document[0x1a6:]))
	if clxLength == 0 || clxAt+clxLength > uint64(len(table)) {
		return errors.New("the Word document's piece table is outside its table stream")
	}
	clx := table[clxAt : clxAt+clxLength]
	// The formatting entries come before the piece table.
	at := 0
	for at < len(clx) && clx[at] == 0x01 {
		if at+3 > len(clx) {
			return errors.New("the Word document's piece table is broken")
		}
		at += 3 + int(binary.LittleEndian.Uint16(clx[at+1:]))
	}
	if at+5 > len(clx) || clx[at] != 0x02 {
		return errors.New("the Word document has no piece table")
	}
	plcLength := uint64(binary.LittleEndian.Uint32(clx[at+1:]))
	if uint64(at+5)+plcLength > uint64(len(clx)) || plcLength < 16 {
		return errors.New("the Word document's piece table is broken")
	}
	plc := clx[at+5 : uint64(at+5)+plcLength]
	pieces := (len(plc) - 4) / 12
	lines := &legacyLines{collected: collected}
	// Fields nest: a code runs from 0x13 to 0x14, what it shows to 0x15.
	fields := []bool{}
	inCode := func() bool {
		for _, code := range fields {
			if code {
				return true
			}
		}
		return false
	}
	put := func(character rune) {
		switch {
		case character == 0x13:
			fields = append(fields, true)
		case character == 0x14 && len(fields) > 0:
			fields[len(fields)-1] = false
		case character == 0x15 && len(fields) > 0:
			fields = fields[:len(fields)-1]
		case inCode() || len(fields) > 64:
		case character == 0x0d || character == 0x07 || character == 0x0b || character == 0x0c:
			lines.put('\n')
		case character == 0x09 || character == 0xa0:
			lines.put(' ')
		case character == 0x1e:
			lines.put('-')
		case character < 0x20:
		default:
			lines.put(character)
		}
	}
	read := 0
	for piece := 0; piece < pieces && !collected.full() && read < fileTextMaxRunes*2; piece++ {
		start, end := binary.LittleEndian.Uint32(plc[piece*4:]), binary.LittleEndian.Uint32(plc[piece*4+4:])
		if end <= start {
			continue
		}
		count := uint64(end - start)
		descriptor := plc[4*(pieces+1)+piece*8:]
		at := binary.LittleEndian.Uint32(descriptor[2:])
		if at&wordPieceSqueeze != 0 {
			offset := uint64(at&^wordPieceSqueeze) / 2
			if offset+count > uint64(len(document)) {
				return errors.New("a Word document piece is outside the document")
			}
			for _, character := range document[offset : offset+count] {
				put(charmap.Windows1252.DecodeByte(character))
			}
		} else {
			offset := uint64(at)
			if offset+count*2 > uint64(len(document)) {
				return errors.New("a Word document piece is outside the document")
			}
			units := make([]uint16, count)
			for index := range units {
				units[index] = binary.LittleEndian.Uint16(document[offset+uint64(index)*2:])
			}
			for _, character := range utf16.Decode(units) {
				put(character)
			}
		}
		read += int(count)
	}
	lines.flush()
	return nil
}

const (
	powerPointMainMaster    = 0x03f8
	powerPointTextChars     = 0x0fa0
	powerPointTextBytes     = 0x0fa8
	powerPointEncryptedUser = 0xf3d1c4df
)

// powerPointText reads a PowerPoint 97–2003 presentation's text records in
// their order — the slides' and the notes' — leaving out the masters'
// placeholder text.
func powerPointText(file *compoundFile, collected *fileTextCollector) error {
	if user, err := file.stream("Current User"); err == nil && len(user) >= 16 &&
		binary.LittleEndian.Uint32(user[12:]) == powerPointEncryptedUser {
		return nil
	}
	stream, err := file.stream("PowerPoint Document")
	if err != nil {
		return err
	}
	lines := &legacyLines{collected: collected}
	var walk func(start, end, depth int)
	walk = func(start, end, depth int) {
		for at := start; at+8 <= end && !collected.full(); {
			version, kind := binary.LittleEndian.Uint16(stream[at:]), binary.LittleEndian.Uint16(stream[at+2:])
			body, length := at+8, int(binary.LittleEndian.Uint32(stream[at+4:]))
			if length < 0 || length > end-body {
				return
			}
			switch {
			case version&0x0f == 0x0f:
				if kind != powerPointMainMaster && depth < 64 {
					walk(body, body+length, depth+1)
				}
			case kind == powerPointTextChars:
				units := make([]uint16, length/2)
				for index := range units {
					units[index] = binary.LittleEndian.Uint16(stream[body+index*2:])
				}
				powerPointLine(lines, []rune(string(utf16.Decode(units))))
			case kind == powerPointTextBytes:
				characters := make([]rune, length)
				for index, character := range stream[body : body+length] {
					characters[index] = rune(character)
				}
				powerPointLine(lines, characters)
			}
			at = body + length
		}
	}
	walk(0, len(stream), 0)
	lines.flush()
	return nil
}

// powerPointLine puts a text record's characters, its paragraphs and
// vertical tabs breaking lines.
func powerPointLine(lines *legacyLines, characters []rune) {
	for _, character := range characters {
		switch {
		case character == '\r' || character == 0x0b:
			lines.put('\n')
		case character == '\t':
			lines.put(' ')
		case character < 0x20:
		default:
			lines.put(character)
		}
	}
	lines.put('\n')
}
