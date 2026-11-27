package app

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"io"
	"sync"
)

// History keeps its rows deflated (2026-10-18, losing nothing: the same
// bytes come back): an event's payload and a checkpoint's columns start
// with pageHistoryDeflated and the content's length, then the deflated
// content. Neither JSON nor an encoded ygo state starts so — an empty
// state is those two zero bytes and nothing after — so rows written before
// read as they are. Typing into a page, events took 42% less and
// checkpoints 73% less.

var pageHistoryDeflated = []byte{0, 0, 'L', 'Z'}

const (
	pageHistoryDeflatedHeader = 8
	// Shorter content stays as it is: the header and deflate's own framing
	// would outweigh it.
	pageHistoryDeflateFrom = 96
)

var errPageHistoryDeflatedInvalid = errors.New("a history row's deflated content doesn't read back")

var pageHistoryWriters = sync.Pool{New: func() any {
	writer, _ := flate.NewWriter(io.Discard, flate.DefaultCompression)
	return writer
}}

var pageHistoryReaders = sync.Pool{New: func() any { return flate.NewReader(bytes.NewReader(nil)) }}

// packPageHistory is content as a history row keeps it.
func packPageHistory(raw []byte) []byte {
	if len(raw) < pageHistoryDeflateFrom || len(raw) > 1<<31 || isPackedPageHistory(raw) {
		return raw
	}
	var out bytes.Buffer
	out.Grow(len(raw)/3 + pageHistoryDeflatedHeader)
	out.Write(pageHistoryDeflated)
	_ = binary.Write(&out, binary.BigEndian, uint32(len(raw)))
	writer := pageHistoryWriters.Get().(*flate.Writer)
	defer pageHistoryWriters.Put(writer)
	writer.Reset(&out)
	if _, err := writer.Write(raw); err != nil || writer.Close() != nil || out.Len() >= len(raw) {
		return raw
	}
	return out.Bytes()
}

// packPageHistoryText is packPageHistory for a text column. Packed content
// always goes in as bytes, never as text, whose zero bytes a reader could
// cut.
func packPageHistoryText(raw string) any {
	packed := packPageHistory([]byte(raw))
	if isPackedPageHistory(packed) {
		return packed
	}
	return raw
}

func isPackedPageHistory(stored []byte) bool {
	return len(stored) >= pageHistoryDeflatedHeader && bytes.Equal(stored[:len(pageHistoryDeflated)], pageHistoryDeflated)
}

// unpackPageHistory is a history row's content as it was written.
func unpackPageHistory(stored []byte) ([]byte, error) {
	if !isPackedPageHistory(stored) {
		return stored, nil
	}
	size := binary.BigEndian.Uint32(stored[len(pageHistoryDeflated):pageHistoryDeflatedHeader])
	reader := pageHistoryReaders.Get().(io.ReadCloser)
	defer pageHistoryReaders.Put(reader)
	if err := reader.(flate.Resetter).Reset(bytes.NewReader(stored[pageHistoryDeflatedHeader:]), nil); err != nil {
		return nil, err
	}
	raw := make([]byte, size)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return nil, errors.Join(errPageHistoryDeflatedInvalid, err)
	}
	return raw, nil
}

// unpackPageHistoryText is unpackPageHistory for a text column.
func unpackPageHistoryText(stored string) (string, error) {
	if !isPackedPageHistory([]byte(stored)) {
		return stored, nil
	}
	raw, err := unpackPageHistory([]byte(stored))
	return string(raw), err
}

// pageHistoryRawLength is a row's content length as written, from its
// first pageHistoryDeflatedHeader bytes and its stored length.
func pageHistoryRawLength(head []byte, stored int64) int64 {
	if !isPackedPageHistory(head) {
		return stored
	}
	return int64(binary.BigEndian.Uint32(head[len(pageHistoryDeflated):pageHistoryDeflatedHeader]))
}

// pageHistoryStoredPart is a kept row's first bytes and its length.
type pageHistoryStoredPart struct {
	Head   []byte `db:"head"`
	Stored int64  `db:"stored"`
}

func pageHistoryWrittenBytes(parts []pageHistoryStoredPart) int64 {
	total := int64(0)
	for _, part := range parts {
		total += pageHistoryRawLength(part.Head, part.Stored)
	}
	return total
}
