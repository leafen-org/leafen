package app

import (
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

// An Excel 97–2003 workbook's words (file_text_legacy_office.go): its sheet
// names, its shared strings — the text of most cells, kept once — and the
// text of cells and formula results kept with them. The shared strings may
// run on through continuation records, where a string's characters may
// switch between one and two bytes each.

const (
	excelBOF          = 0x0809
	excelFilePass     = 0x002f
	excelBoundSheet   = 0x0085
	excelSharedTable  = 0x00fc
	excelContinue     = 0x003c
	excelLabel        = 0x0204
	excelFormulaText  = 0x0207
	excelMaxSegments  = 1 << 16
	excelMaxSharedRun = 1 << 22
)

func excelText(file *compoundFile, collected *fileTextCollector) error {
	book, err := file.stream("Workbook")
	if errors.Is(err, errCompoundStreamMissing) {
		return errors.New("an Excel workbook older than Excel 97")
	}
	if err != nil {
		return err
	}
	if len(book) < 4 || binary.LittleEndian.Uint16(book) != excelBOF {
		return errors.New("not an Excel workbook")
	}
	type record struct {
		kind uint16
		data []byte
	}
	records := []record{}
	for at := 0; at+4 <= len(book); {
		kind, length := binary.LittleEndian.Uint16(book[at:]), int(binary.LittleEndian.Uint16(book[at+2:]))
		if at+4+length > len(book) {
			break
		}
		records = append(records, record{kind, book[at+4 : at+4+length]})
		at += 4 + length
	}
	lines := &legacyLines{collected: collected}
	for index := 0; index < len(records) && !collected.full(); index++ {
		current := records[index]
		switch current.kind {
		case excelFilePass:
			return nil
		case excelBoundSheet:
			if len(current.data) >= 8 {
				reader := &excelReader{segments: [][]byte{current.data[8:]}}
				name, _ := reader.characters(int(current.data[6]), current.data[7]&1 != 0)
				lines.collected.add(name)
			}
		case excelLabel, excelFormulaText:
			data := current.data
			if current.kind == excelLabel {
				if len(data) < 6 {
					continue
				}
				data = data[6:]
			}
			if len(data) >= 3 {
				reader := &excelReader{segments: [][]byte{data[3:]}}
				text, _ := reader.characters(int(binary.LittleEndian.Uint16(data)), data[2]&1 != 0)
				lines.collected.add(text)
			}
		case excelSharedTable:
			segments := [][]byte{current.data}
			for index+1 < len(records) && records[index+1].kind == excelContinue && len(segments) < excelMaxSegments {
				index++
				segments = append(segments, records[index].data)
			}
			excelSharedStrings(&excelReader{segments: segments}, lines)
		}
	}
	lines.flush()
	return nil
}

// excelSharedStrings reads the shared strings table, a string a line.
func excelSharedStrings(reader *excelReader, lines *legacyLines) {
	if !reader.skip(4) {
		return
	}
	unique, ok := reader.uint32()
	for count := uint32(0); ok && count < unique && count < excelMaxSharedRun && !lines.collected.full(); count++ {
		var length uint16
		var flags byte
		if length, ok = reader.uint16(); !ok {
			return
		}
		if flags, ok = reader.byte(); !ok {
			return
		}
		runs, extended := uint16(0), uint32(0)
		if flags&0x08 != 0 {
			if runs, ok = reader.uint16(); !ok {
				return
			}
		}
		if flags&0x04 != 0 {
			if extended, ok = reader.uint32(); !ok {
				return
			}
		}
		text, complete := reader.characters(int(length), flags&0x01 != 0)
		lines.collected.add(text)
		if !complete || !reader.skip(int(runs)*4) || !reader.skip(int(extended)) {
			return
		}
	}
}

// excelReader reads a record's data and its continuations as one, but for
// characters, which start each continuation with their width again.
type excelReader struct {
	segments [][]byte
	segment  int
	at       int
}

func (reader *excelReader) byte() (byte, bool) {
	for reader.segment < len(reader.segments) && reader.at >= len(reader.segments[reader.segment]) {
		reader.segment, reader.at = reader.segment+1, 0
	}
	if reader.segment >= len(reader.segments) {
		return 0, false
	}
	value := reader.segments[reader.segment][reader.at]
	reader.at++
	return value, true
}

func (reader *excelReader) uint16() (uint16, bool) {
	low, ok := reader.byte()
	high, more := reader.byte()
	return uint16(low) | uint16(high)<<8, ok && more
}

func (reader *excelReader) uint32() (uint32, bool) {
	low, ok := reader.uint16()
	high, more := reader.uint16()
	return uint32(low) | uint32(high)<<16, ok && more
}

func (reader *excelReader) skip(count int) bool {
	for ; count > 0; count-- {
		if _, ok := reader.byte(); !ok {
			return false
		}
	}
	return true
}

// characters reads count characters, one byte each or two, a continuation
// setting the width anew; it says whether all were there.
func (reader *excelReader) characters(count int, wide bool) (string, bool) {
	units := make([]uint16, 0, min(count, 1<<16))
	for ; count > 0; count-- {
		if reader.segment < len(reader.segments) && reader.at >= len(reader.segments[reader.segment]) {
			reader.segment, reader.at = reader.segment+1, 0
			flags, ok := reader.byte()
			if !ok {
				return string(utf16.Decode(units)), false
			}
			wide = flags&0x01 != 0
		}
		var unit uint16
		ok := false
		if wide {
			unit, ok = reader.uint16()
		} else {
			var low byte
			low, ok = reader.byte()
			unit = uint16(low)
		}
		if !ok {
			return string(utf16.Decode(units)), false
		}
		units = append(units, unit)
	}
	return string(utf16.Decode(units)), true
}
