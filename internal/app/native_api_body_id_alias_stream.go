package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// The IDs a JSON body names at its top level, and the people it names at any
// depth (native_api_person_id_aliases.go), are read the same way however
// large the body is, and whether or not it says its length: a body read
// whole before its route only up to a bound (256 KiB) left a page's ID in
// another form unread in a larger one — a long Markdown with its pageId, a
// batch of blocks — and the route answered that the page wasn't found
// (2026-11-23). A body is rewritten as it is read, by whichever reader comes
// first (a key's checks, the route): every byte passes as it came but the
// string values of the fields resolved (nativeBodyIDAliasFields at the top
// level, nativePersonIDFields at any). Nothing more of the body is held than
// one piece read, one ID value (nativeBodyAliasCaptureMax) and which objects
// and lists are open; the server keeps the body read, as it keeps every
// request's, for the route to read it again.

// nativeBodyAliasCaptureMax bounds an ID value held to resolve: an ID is at
// most maxGraphIdentifierLength bytes, and a longer value names none and
// passes as it came.
const nativeBodyAliasCaptureMax = 4 << 10

// nativeBodyAliasKeyMax bounds a key read to tell whether it names an ID
// field; a longer one names none.
const nativeBodyAliasKeyMax = 64

// nativeBodyAliasDepthMax bounds the objects and lists followed open at
// once, as Go's JSON reading bounds them (10000): past it the rest of the
// body passes as it came, for its route to refuse.
const nativeBodyAliasDepthMax = 10000

// nativeBodyAliasScanner rewrites a JSON body given in pieces: the string
// value of each top-level field nativeBodyIDAliasFields names, and of each
// field nativePersonIDFields names at any depth (a string, or each string of
// a list), becomes what resolve gives for it; with mutationID, a top-level
// mutationId left out or empty becomes it (native_api_body_mutation_id.go).
// A body that isn't a JSON object passes as it came.
type nativeBodyAliasScanner struct {
	resolve func(name, value string) string
	// mutationID is the one the server made for a body that leaves its own
	// out; topFields counts the body's keys, and mutationSeen says it named one.
	mutationID   string
	topFields    int
	mutationSeen bool
	// frames are the objects and arrays open, the innermost last.
	frames           []nativeBodyAliasFrame
	done             bool
	inString, escape bool
	// In the innermost object: a key is next, one is being read, its value
	// is next; and whether the key read names a field resolved, kept by name
	// only then.
	expectKey, readingKey, valueNext bool
	key                              []byte
	keyName                          string
	keyAlias, keyPerson, keyMutation bool
	// The ID value being held, as written, and the field it resolves as.
	capturing   bool
	captureName string
	captured    []byte
}

// nativeBodyAliasFrame is an open object or array; people names the person
// field a list is the value of, whose strings are people's IDs.
type nativeBodyAliasFrame struct {
	object bool
	people string
}

// feed appends what the piece in becomes to out.
func (s *nativeBodyAliasScanner) feed(in, out []byte) []byte {
	for i := 0; i < len(in); {
		if s.done {
			return append(out, in[i:]...)
		}
		c := in[i]
		if s.capturing {
			i++
			s.captured = append(s.captured, c)
			switch {
			case s.escape:
				s.escape = false
			case c == '\\':
				s.escape = true
			case c == '"':
				s.capturing, s.inString = false, false
				out = append(out, s.resolvedValue()...)
				s.captured = s.captured[:0]
			case len(s.captured) > nativeBodyAliasCaptureMax:
				// Too long to be an ID: the rest of the string passes as it comes.
				s.capturing = false
				out = append(out, s.captured...)
				s.captured = s.captured[:0]
			}
			continue
		}
		if s.inString {
			if !s.escape && !s.readingKey {
				next := bytes.IndexAny(in[i:], "\"\\")
				if next < 0 {
					return append(out, in[i:]...)
				}
				out = append(out, in[i:i+next]...)
				i += next
				c = in[i]
			}
			i++
			out = append(out, c)
			if s.readingKey && len(s.key) <= nativeBodyAliasKeyMax {
				s.key = append(s.key, c)
			}
			switch {
			case s.escape:
				s.escape = false
			case c == '\\':
				s.escape = true
			case c == '"':
				s.inString = false
				if s.readingKey {
					s.readingKey = false
					s.keyRead()
				}
			}
			continue
		}
		// Inside the body, only strings, brackets, colons and commas matter;
		// anything else between them is a value (a number, true, false or
		// null) or space.
		if len(s.frames) > 0 {
			next := bytes.IndexAny(in[i:], "\"{}[]:,")
			span := in[i:]
			if next >= 0 {
				span = in[i : i+next]
			}
			if len(bytes.Trim(span, " \t\r\n")) > 0 {
				s.valueNext = false
			}
			out = append(out, span...)
			if next < 0 {
				return out
			}
			i += next
			c = in[i]
		}
		i++
		if c == '"' {
			if name, held := s.heldValue(); held {
				s.capturing, s.inString, s.valueNext = true, true, false
				s.captureName = name
				s.captured = append(s.captured[:0], c)
				continue
			}
		}
		if c == '}' && len(s.frames) == 1 && s.mutationID != "" && !s.mutationSeen {
			// The body's end, which named no mutationId: the one made.
			if s.topFields > 0 {
				out = append(out, ',')
			}
			out = append(out, `"`+nativeMutationIDField+`":"`+s.mutationID+`"`...)
		}
		out = append(out, c)
		s.step(c)
	}
	return out
}

// inObject is whether the innermost open frame is an object.
func (s *nativeBodyAliasScanner) inObject() bool {
	return len(s.frames) > 0 && s.frames[len(s.frames)-1].object
}

// heldValue is the field a string starting now is the value of, if it is
// one resolved: a top-level ID field's, a person field's, or an entry of a
// person field's list.
func (s *nativeBodyAliasScanner) heldValue() (string, bool) {
	if len(s.frames) == 0 {
		return "", false
	}
	if frame := s.frames[len(s.frames)-1]; !frame.object {
		return frame.people, frame.people != ""
	}
	if s.valueNext && s.keyMutation {
		return nativeMutationIDField, true
	}
	return s.keyName, s.valueNext && (s.keyAlias || s.keyPerson)
}

// keyRead notes whether the key just read names a field resolved: an ID
// field at the top level, a person field at any depth, or the body's own
// mutationId where the server makes one for a body that leaves it out.
func (s *nativeBodyAliasScanner) keyRead() {
	s.keyName, s.keyAlias, s.keyPerson, s.keyMutation = "", false, false, false
	top := len(s.frames) == 1
	if top {
		s.topFields++
	}
	if len(s.key) < 2 || len(s.key) > nativeBodyAliasKeyMax {
		return
	}
	name := ""
	if raw := s.key[1 : len(s.key)-1]; bytes.IndexByte(raw, '\\') < 0 {
		// Most keys name neither, and are told so without being copied.
		_, alias := nativeBodyIDAliasFields[string(raw)]
		if !alias && !nativePersonIDFields[string(raw)] && !(top && s.mutationID != "" && nativeMutationIDKey(string(raw))) {
			return
		}
		name = string(raw)
	} else if json.Unmarshal(s.key, &name) != nil {
		return
	}
	if top && s.mutationID != "" && nativeMutationIDKey(name) {
		// The body's own, kept as given; an empty one is the one made.
		s.mutationSeen, s.keyMutation = true, true
		return
	}
	_, alias := nativeBodyIDAliasFields[name]
	s.keyAlias = alias && len(s.frames) == 1
	s.keyPerson = nativePersonIDFields[name]
	if s.keyAlias || s.keyPerson {
		s.keyName = name
	}
}

// step follows a byte outside strings other than an ID value's start.
func (s *nativeBodyAliasScanner) step(c byte) {
	switch c {
	case '"':
		s.inString = true
		if s.inObject() && s.expectKey {
			s.expectKey, s.readingKey = false, true
			s.key = append(s.key[:0], c)
		} else {
			s.valueNext = false
		}
	case '{', '[':
		if len(s.frames) == 0 {
			// The body itself: only an object has fields.
			s.done = c != '{'
		}
		if len(s.frames) >= nativeBodyAliasDepthMax {
			s.done = true
			return
		}
		frame := nativeBodyAliasFrame{object: c == '{'}
		if c == '[' && s.inObject() && s.valueNext && s.keyPerson {
			frame.people = s.keyName
		}
		s.frames = append(s.frames, frame)
		s.expectKey, s.valueNext = c == '{', false
	case '}', ']':
		if len(s.frames) > 0 {
			s.frames = s.frames[:len(s.frames)-1]
		}
		s.done = len(s.frames) == 0
		s.expectKey, s.valueNext = false, false
	case ':':
		s.valueNext = s.inObject()
	case ',':
		s.expectKey, s.valueNext = s.inObject(), false
	case ' ', '\t', '\r', '\n':
	default:
		// A number, true, false or null, or what no JSON starts with.
		s.valueNext = false
		s.done = s.done || len(s.frames) == 0
	}
}

// resolvedValue is the held ID value as resolved, written as JSON, or as
// it was written when it names nothing else.
func (s *nativeBodyAliasScanner) resolvedValue() []byte {
	value := ""
	if json.Unmarshal(s.captured, &value) != nil {
		return s.captured
	}
	if s.captureName == nativeMutationIDField {
		if strings.TrimSpace(value) == "" {
			return []byte(`"` + s.mutationID + `"`)
		}
		return s.captured
	}
	if value == "" {
		return s.captured
	}
	resolved := s.resolve(s.captureName, value)
	if resolved == value {
		return s.captured
	}
	encoded, err := json.Marshal(resolved)
	if err != nil {
		return s.captured
	}
	return encoded
}

// flush appends what is still held at the body's end — a value its end
// cut — as it was written.
func (s *nativeBodyAliasScanner) flush(out []byte) []byte {
	if s.capturing {
		out = append(out, s.captured...)
		s.capturing = false
		s.captured = s.captured[:0]
	}
	return out
}

// nativeBodyAliasReader is a request body read through the scanner.
type nativeBodyAliasReader struct {
	source  io.ReadCloser
	scan    nativeBodyAliasScanner
	piece   []byte
	pending []byte
	err     error
}

func (r *nativeBodyAliasReader) Read(p []byte) (int, error) {
	for len(r.pending) == 0 && r.err == nil {
		if r.piece == nil {
			r.piece = make([]byte, 32<<10)
		}
		n, err := r.source.Read(r.piece)
		r.pending = r.scan.feed(r.piece[:n], r.pending[:0])
		if err != nil {
			r.pending, r.err = r.scan.flush(r.pending), err
		}
	}
	if len(r.pending) == 0 {
		return 0, r.err
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *nativeBodyAliasReader) Close() error {
	return r.source.Close()
}
