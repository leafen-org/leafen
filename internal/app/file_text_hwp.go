package app

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
)

// The words of Hangul documents reach search as those of Office files do
// (file_text_extraction.go): an HWPX file is a zip of XML sections, whose
// paragraphs' runs hold the text; an HWP 5 file is a compound file whose
// BodyText sections hold records, compressed unless the header says not, and
// whose paragraph text records hold the text with its controls among it. A
// file locked by a password or made for distribution only keeps its words.

// hwpxText reads an HWPX file's sections in their order.
func hwpxText(archive *zip.Reader, collected *fileTextCollector) error {
	sections := map[int]*zip.File{}
	for _, file := range archive.File {
		name := strings.TrimSuffix(strings.TrimPrefix(file.Name, "Contents/section"), ".xml")
		if number, err := strconv.Atoi(name); err == nil && strings.HasPrefix(file.Name, "Contents/section") && number >= 0 {
			sections[number] = file
		}
	}
	for number := 0; number < len(sections) && !collected.full(); number++ {
		file, ok := sections[number]
		if !ok {
			break
		}
		reader, err := file.Open()
		if err != nil {
			return err
		}
		err = xmlText(io.LimitReader(reader, fileTextMaxPartBytes), map[string]bool{"t": true}, map[string]bool{"p": true}, "", collected)
		reader.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

const (
	hwpTagParaText      = 67
	hwpHeaderCompressed = 1 << 0
	hwpHeaderPassword   = 1 << 1
	hwpHeaderDistribute = 1 << 2
	hwpMaxSections      = 4096
)

// hwpText reads an HWP 5 file's sections in their order.
func hwpText(source io.ReaderAt, size int64, collected *fileTextCollector) error {
	file, err := openCompoundFile(source, size)
	if err != nil {
		return err
	}
	header, err := file.stream("FileHeader")
	if err != nil {
		return err
	}
	if len(header) < 40 || !bytes.HasPrefix(header, []byte("HWP Document File")) {
		return errors.New("not an HWP 5 file")
	}
	properties := binary.LittleEndian.Uint32(header[36:40])
	if properties&(hwpHeaderPassword|hwpHeaderDistribute) != 0 {
		return nil
	}
	for number := 0; number < hwpMaxSections && !collected.full(); number++ {
		section, err := file.stream("BodyText/Section" + strconv.Itoa(number))
		if errors.Is(err, errCompoundStreamMissing) {
			break
		}
		if err != nil {
			return err
		}
		var records io.ReadCloser = io.NopCloser(bytes.NewReader(section))
		if properties&hwpHeaderCompressed != 0 {
			records = flate.NewReader(bytes.NewReader(section))
		}
		err = hwpSectionText(io.LimitReader(records, fileTextMaxPartBytes), collected)
		records.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// hwpSectionText reads a section's paragraph text records.
func hwpSectionText(records io.Reader, collected *fileTextCollector) error {
	var word [4]byte
	for !collected.full() {
		if _, err := io.ReadFull(records, word[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		header := binary.LittleEndian.Uint32(word[:])
		tag, length := header&0x3ff, int64(header>>20)
		if length == 0xfff {
			if _, err := io.ReadFull(records, word[:]); err != nil {
				return nil
			}
			length = int64(binary.LittleEndian.Uint32(word[:]))
		}
		if length > fileTextMaxPartBytes {
			return errors.New("an HWP record is too large")
		}
		if tag != hwpTagParaText {
			if _, err := io.CopyN(io.Discard, records, length); err != nil {
				return nil
			}
			continue
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(records, body); err != nil {
			return nil
		}
		collected.add(hwpParagraphText(body))
	}
	return nil
}

// hwpParagraphText decodes a paragraph's UTF-16 text, leaving out its
// controls: an inline or extended control takes eight units, a character
// control one; a line break, a tab and a fixed space read as themselves.
func hwpParagraphText(body []byte) string {
	units := make([]uint16, 0, len(body)/2)
	for index := 0; index+1 < len(body); {
		unit := binary.LittleEndian.Uint16(body[index:])
		switch {
		case unit >= 32:
			units = append(units, unit)
			index += 2
		case unit == 10 || unit == 13:
			units = append(units, '\n')
			index += 2
		case unit == 9:
			units = append(units, ' ')
			index += 16
		case unit == 30 || unit == 31:
			units = append(units, ' ')
			index += 2
		case unit == 0 || unit >= 24:
			index += 2
		default:
			index += 16
		}
	}
	return string(utf16.Decode(units))
}

var errCompoundStreamMissing = errors.New("no such stream in the compound file")

const (
	compoundEndOfChain   = 0xfffffffe
	compoundMaxDirectory = 1 << 16
)

// compoundFile reads the streams of a compound file, as an HWP 5 file is
// kept: sectors chained by an allocation table, small streams in a mini
// stream with its own table, and a directory of named entries.
type compoundFile struct {
	source        io.ReaderAt
	size          int64
	sectorSize    int64
	miniSize      int64
	miniCutoff    uint64
	fat, miniFAT  []uint32
	miniStream    []byte
	entries       []compoundEntry
	pathsToStream map[string]int
}

type compoundEntry struct {
	name               string
	kind               byte
	left, right, child uint32
	start              uint32
	length             uint64
}

func openCompoundFile(source io.ReaderAt, size int64) (*compoundFile, error) {
	header := make([]byte, 512)
	if _, err := source.ReadAt(header, 0); err != nil {
		return nil, err
	}
	if !bytes.Equal(header[:8], []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
		return nil, errors.New("not a compound file")
	}
	sectorShift, miniShift := binary.LittleEndian.Uint16(header[0x1e:]), binary.LittleEndian.Uint16(header[0x20:])
	if sectorShift != 9 && sectorShift != 12 || miniShift != 6 {
		return nil, errors.New("unknown compound file sector size")
	}
	file := &compoundFile{
		source: source, size: size, sectorSize: 1 << sectorShift, miniSize: 1 << miniShift,
		miniCutoff: uint64(binary.LittleEndian.Uint32(header[0x38:])),
	}
	// The allocation table's sectors: the header's first 109, then a chain.
	fatSectors := []uint32{}
	for index := 0; index < 109; index++ {
		sector := binary.LittleEndian.Uint32(header[0x4c+index*4:])
		if sector < compoundEndOfChain {
			fatSectors = append(fatSectors, sector)
		}
	}
	perSector := int(file.sectorSize / 4)
	next := binary.LittleEndian.Uint32(header[0x44:])
	for hops := 0; next < compoundEndOfChain; hops++ {
		if hops > int(size/file.sectorSize) {
			return nil, errors.New("the compound file's table chain loops")
		}
		sector, err := file.sector(next)
		if err != nil {
			return nil, err
		}
		for index := 0; index < perSector-1; index++ {
			if value := binary.LittleEndian.Uint32(sector[index*4:]); value < compoundEndOfChain {
				fatSectors = append(fatSectors, value)
			}
		}
		next = binary.LittleEndian.Uint32(sector[(perSector-1)*4:])
	}
	for _, number := range fatSectors {
		sector, err := file.sector(number)
		if err != nil {
			return nil, err
		}
		for index := 0; index < perSector; index++ {
			file.fat = append(file.fat, binary.LittleEndian.Uint32(sector[index*4:]))
		}
	}
	directory, err := file.chain(binary.LittleEndian.Uint32(header[0x30:]), 0)
	if err != nil {
		return nil, err
	}
	for offset := 0; offset+128 <= len(directory) && len(file.entries) < compoundMaxDirectory; offset += 128 {
		entry := directory[offset : offset+128]
		// The name's length counts its closing null.
		units := make([]uint16, 0, 32)
		for index := 0; index < int(binary.LittleEndian.Uint16(entry[0x40:]))/2-1 && index < 32; index++ {
			units = append(units, binary.LittleEndian.Uint16(entry[index*2:]))
		}
		file.entries = append(file.entries, compoundEntry{
			name: string(utf16.Decode(units)), kind: entry[0x42],
			left: binary.LittleEndian.Uint32(entry[0x44:]), right: binary.LittleEndian.Uint32(entry[0x48:]),
			child: binary.LittleEndian.Uint32(entry[0x4c:]), start: binary.LittleEndian.Uint32(entry[0x74:]),
			length: binary.LittleEndian.Uint64(entry[0x78:]),
		})
	}
	if len(file.entries) == 0 || file.entries[0].kind != 5 {
		return nil, errors.New("the compound file has no root")
	}
	if sectorShift == 9 {
		for index := range file.entries {
			file.entries[index].length &= 0xffffffff
		}
	}
	file.pathsToStream = map[string]int{}
	file.walk(file.entries[0].child, "", 0)
	root := file.entries[0]
	if root.length > 0 {
		if file.miniStream, err = file.chain(root.start, root.length); err != nil {
			return nil, err
		}
		miniTable, err := file.chain(binary.LittleEndian.Uint32(header[0x3c:]), 0)
		if err != nil {
			return nil, err
		}
		for index := 0; index+4 <= len(miniTable); index += 4 {
			file.miniFAT = append(file.miniFAT, binary.LittleEndian.Uint32(miniTable[index:]))
		}
	}
	return file, nil
}

// walk names each stream by its path, through the directory's sibling trees.
func (file *compoundFile) walk(id uint32, prefix string, depth int) {
	if id >= uint32(len(file.entries)) || depth > 64 {
		return
	}
	entry := file.entries[id]
	file.walk(entry.left, prefix, depth+1)
	file.walk(entry.right, prefix, depth+1)
	switch entry.kind {
	case 1:
		file.walk(entry.child, prefix+entry.name+"/", depth+1)
	case 2:
		if _, known := file.pathsToStream[prefix+entry.name]; !known {
			file.pathsToStream[prefix+entry.name] = int(id)
		}
	}
}

func (file *compoundFile) sector(number uint32) ([]byte, error) {
	offset := (int64(number) + 1) * file.sectorSize
	if offset < 0 || offset+file.sectorSize > file.size+file.sectorSize {
		return nil, errors.New("a compound file sector is outside the file")
	}
	sector := make([]byte, file.sectorSize)
	read, err := file.source.ReadAt(sector, offset)
	if err != nil && !(errors.Is(err, io.EOF) && read > 0) {
		return nil, err
	}
	return sector, nil
}

// chain reads the sectors from start on, up to length bytes when given.
func (file *compoundFile) chain(start uint32, length uint64) ([]byte, error) {
	data := []byte{}
	for sector, hops := start, 0; sector < compoundEndOfChain; hops++ {
		if hops > len(file.fat) || int64(len(data)) > fileTextMaxSourceBytes {
			return nil, errors.New("the compound file's chain loops")
		}
		block, err := file.sector(sector)
		if err != nil {
			return nil, err
		}
		data = append(data, block...)
		if int(sector) >= len(file.fat) {
			break
		}
		sector = file.fat[sector]
	}
	if length > 0 && uint64(len(data)) > length {
		data = data[:length]
	}
	return data, nil
}

// stream reads the stream at a path, from the mini stream when it is small.
func (file *compoundFile) stream(path string) ([]byte, error) {
	id, ok := file.pathsToStream[path]
	if !ok {
		return nil, errCompoundStreamMissing
	}
	entry := file.entries[id]
	if entry.length >= file.miniCutoff {
		return file.chain(entry.start, entry.length)
	}
	data := make([]byte, 0, entry.length)
	for sector, hops := entry.start, 0; sector < compoundEndOfChain && uint64(len(data)) < entry.length; hops++ {
		offset := int64(sector) * file.miniSize
		if hops > len(file.miniFAT) || offset+file.miniSize > int64(len(file.miniStream)) || int(sector) >= len(file.miniFAT) {
			return nil, errors.New("the compound file's mini stream is broken")
		}
		data = append(data, file.miniStream[offset:offset+file.miniSize]...)
		sector = file.miniFAT[sector]
	}
	if uint64(len(data)) > entry.length {
		data = data[:entry.length]
	}
	return data, nil
}
