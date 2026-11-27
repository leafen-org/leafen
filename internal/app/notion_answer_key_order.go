package app

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"sync"
)

// Notion's API writes each kind of object's keys in one order, request after
// request (seen 2026-11-11, Notion-Version 2026-03-11): a page's, a block's,
// a database's, a data source's, a list's, a user's, a property item's, a
// view's, a file upload's, an error's, and those of the objects in them —
// a parent, an icon, a property's value or its schema, a block's own
// object, a rich text item. Leafen makes most of them as maps, whose keys
// the encoder writes in a new order each time, so every public API answer
// is put in Notion's order here, at the envelope every answer passes
// (serveNotionAnswer), by what each object is (notionJSONRoles); the same
// pass names the answer's request_id as its last key. An object's keys an
// order doesn't name follow its named ones as written, and an object of no
// known kind keeps the order it was written in. The answer is read once
// into a flat list of its values, which is kept for the next answer.

// notionJSONNode is one value of an answer: where it and the key that holds
// it are written, and the index just after the values in it.
type notionJSONNode struct {
	start, end, key, keyEnd, next int32
	open                          byte
	escapedKey, placed            bool
}

// notionJSONAnswer is an answer being put in order.
type notionJSONAnswer struct {
	body  []byte
	nodes []notionJSONNode
	out   bytes.Buffer
	kind  [64]byte
}

var notionJSONAnswers = sync.Pool{New: func() any { return &notionJSONAnswer{} }}

// notionJSONDepth bounds how deep an answer is read; a deeper one is
// answered as it is.
const notionJSONDepth = 512

// notionAnswerShaped is a JSON answer with its objects in Notion's orders
// and, when withID, its request_id as its last key; a body that isn't one
// JSON object or array is answered as it is.
func notionAnswerShaped(body []byte, requestID string, withID bool) []byte {
	start := notionJSONSpace(body, 0)
	if start >= len(body) || body[start] != '{' && body[start] != '[' || len(body) >= math.MaxInt32 {
		return body
	}
	answer := notionJSONAnswers.Get().(*notionJSONAnswer)
	defer func() {
		answer.body, answer.nodes = nil, answer.nodes[:0]
		answer.out = bytes.Buffer{}
		notionJSONAnswers.Put(answer)
	}()
	answer.body = body
	end, ok := answer.read(start, 0, 0, false, 0)
	if !ok || notionJSONSpace(body, end) != len(body) {
		return body
	}
	answer.out.Grow(len(body) + 64)
	if answer.nodes[0].open != '{' || !withID {
		answer.write(0, nil)
		return answer.out.Bytes()
	}
	// Whatever request_id the endpoint wrote gives way to the request's own.
	for child := 1; child < len(answer.nodes); child = int(answer.nodes[child].next) {
		answer.nodes[child].placed = answer.keyIs(child, "request_id")
	}
	answer.write(0, nil)
	answer.out.Truncate(answer.out.Len() - 1)
	if answer.out.Len() > 1 {
		answer.out.WriteByte(',')
	}
	quoted, _ := json.Marshal(requestID)
	answer.out.WriteString(`"request_id":`)
	answer.out.Write(quoted)
	answer.out.WriteByte('}')
	return answer.out.Bytes()
}

// read reads the value at i, held by the key from key to keyEnd; the index
// after it, and false for what isn't JSON.
func (answer *notionJSONAnswer) read(i, key, keyEnd int, escapedKey bool, depth int) (int, bool) {
	body := answer.body
	if i >= len(body) || depth > notionJSONDepth {
		return i, false
	}
	index := len(answer.nodes)
	answer.nodes = append(answer.nodes, notionJSONNode{
		start: int32(i), key: int32(key), keyEnd: int32(keyEnd), open: body[i], escapedKey: escapedKey,
	})
	end := i
	switch body[i] {
	case '{', '[':
		closing := byte('}')
		if body[i] == '[' {
			closing = ']'
		}
		i = notionJSONSpace(body, i+1)
		for done := i < len(body) && body[i] == closing; !done; {
			key, keyEnd, escaped := 0, 0, false
			if closing == '}' {
				keyEnd = notionJSONStringEnd(body, i)
				if keyEnd < 0 {
					return i, false
				}
				key, escaped = i, bytes.IndexByte(body[i:keyEnd], '\\') >= 0
				if i = notionJSONSpace(body, keyEnd); i >= len(body) || body[i] != ':' {
					return i, false
				}
				i = notionJSONSpace(body, i+1)
			}
			next, ok := answer.read(i, key, keyEnd, escaped, depth+1)
			if i = notionJSONSpace(body, next); !ok || i >= len(body) {
				return i, false
			}
			switch body[i] {
			case closing:
				done = true
			case ',':
				i = notionJSONSpace(body, i+1)
			default:
				return i, false
			}
		}
		end = i + 1
	case '"':
		if end = notionJSONStringEnd(body, i); end < 0 {
			return i, false
		}
		answer.nodes[index].open = 0
	default:
		for end < len(body) && bytes.IndexByte(notionJSONValueStops, body[end]) < 0 {
			end++
		}
		answer.nodes[index].open = 0
		if end == i {
			return i, false
		}
	}
	answer.nodes[index].end, answer.nodes[index].next = int32(end), int32(len(answer.nodes))
	return end, true
}

// write writes the value at index, an object in the order its role names
// (one with none is found by what it is: objectRole), an array's items each
// in the array's role.
func (answer *notionJSONAnswer) write(index int, role *notionJSONRole) {
	node := answer.nodes[index]
	out := &answer.out
	if node.open == 0 {
		out.Write(answer.body[node.start:node.end])
		return
	}
	first := true
	place := func(child int, role *notionJSONRole) {
		if !first {
			out.WriteByte(',')
		}
		first = false
		if node.open == '{' {
			out.Write(answer.body[answer.nodes[child].key:answer.nodes[child].keyEnd])
			out.WriteByte(':')
		}
		answer.write(child, role)
	}
	out.WriteByte(node.open)
	if node.open == '[' {
		for child := index + 1; child < int(node.next); child = int(answer.nodes[child].next) {
			place(child, role)
		}
		out.WriteByte(']')
		return
	}
	if role == nil {
		role = answer.objectRole(index)
	}
	kind := answer.stringMember(index, "type")
	if role != nil && role.byType != nil {
		if variant, named := role.byType[string(kind)]; named {
			role = variant
		}
	}
	if role != nil {
		for _, key := range role.keys {
			for child := index + 1; child < int(node.next); child = int(answer.nodes[child].next) {
				if !answer.nodes[child].placed && (key == "*" && len(kind) > 0 && bytes.Equal(answer.keyOf(child), kind) ||
					answer.keyIs(child, key)) {
					answer.nodes[child].placed = true
					place(child, answer.memberRole(role, child, kind))
				}
			}
		}
	}
	for child := index + 1; child < int(node.next); child = int(answer.nodes[child].next) {
		if !answer.nodes[child].placed {
			answer.nodes[child].placed = true
			place(child, answer.memberRole(role, child, kind))
		}
	}
	out.WriteByte('}')
}

// objectRole is the role of an object given none: a Notion object by its
// "object", a rich text item by the keys every one has.
func (answer *notionJSONAnswer) objectRole(index int) *notionJSONRole {
	if object := answer.stringMember(index, "object"); len(object) > 0 {
		if role, named := notionJSONObjectRoles[string(object)]; named {
			return notionJSONRoles[role]
		}
	}
	found := 0
	for child := index + 1; child < int(answer.nodes[index].next); child = int(answer.nodes[child].next) {
		if answer.keyIs(child, "plain_text") || answer.keyIs(child, "annotations") || answer.keyIs(child, "type") {
			found++
		}
	}
	if found == 3 {
		return notionJSONRoles["rich text"]
	}
	return nil
}

// memberRole is the role of what an object's key holds, by the object's
// role and type kind.
func (answer *notionJSONAnswer) memberRole(role *notionJSONRole, child int, kind []byte) *notionJSONRole {
	if role == nil {
		return nil
	}
	key := answer.keyOf(child)
	if name, named := role.members[string(key)]; named {
		return notionJSONRoles[name]
	}
	if len(kind) > 0 && bytes.Equal(key, kind) {
		if name, named := role.members["*"]; named {
			if !strings.HasSuffix(name, ":") {
				return notionJSONRoles[name]
			}
			return notionJSONRoles[string(append(append(answer.kind[:0], name...), kind...))]
		}
	}
	return notionJSONRoles[role.each]
}

// keyOf is the key that holds the value at child, read; none in an array.
func (answer *notionJSONAnswer) keyOf(child int) []byte {
	node := answer.nodes[child]
	if node.keyEnd-node.key < 2 {
		return nil
	}
	if !node.escapedKey {
		return answer.body[node.key+1 : node.keyEnd-1]
	}
	name, _ := notionJSONString(answer.body[node.key:node.keyEnd])
	return []byte(name)
}

// keyIs is whether the key that holds the value at child is name.
func (answer *notionJSONAnswer) keyIs(child int, name string) bool {
	return string(answer.keyOf(child)) == name
}

// stringMember is the string the object at index holds at key, read; none
// for no string there.
func (answer *notionJSONAnswer) stringMember(index int, key string) []byte {
	for child := index + 1; child < int(answer.nodes[index].next); child = int(answer.nodes[child].next) {
		if answer.keyIs(child, key) {
			raw := answer.body[answer.nodes[child].start:answer.nodes[child].end]
			if len(raw) < 2 || raw[0] != '"' {
				return nil
			}
			if bytes.IndexByte(raw, '\\') < 0 {
				return raw[1 : len(raw)-1]
			}
			text, _ := notionJSONString(raw)
			return []byte(text)
		}
	}
	return nil
}

// notionJSONString reads a JSON string as written; false for anything else.
func notionJSONString(raw []byte) (string, bool) {
	if len(raw) < 2 || raw[0] != '"' {
		return "", false
	}
	if bytes.IndexByte(raw, '\\') < 0 {
		return string(raw[1 : len(raw)-1]), true
	}
	text := ""
	return text, json.Unmarshal(raw, &text) == nil
}

// notionJSONValueStops are what end a number or a literal.
var notionJSONValueStops = []byte(",}] \n\r\t")

// notionJSONSpace is the first index from i that isn't JSON whitespace.
func notionJSONSpace(value []byte, i int) int {
	for i < len(value) && (value[i] == ' ' || value[i] == '\n' || value[i] == '\r' || value[i] == '\t') {
		i++
	}
	return i
}

// notionJSONStringEnd is the index just after the string that starts at i,
// or -1 when none does.
func notionJSONStringEnd(value []byte, i int) int {
	if i >= len(value) || value[i] != '"' {
		return -1
	}
	for j := i + 1; j < len(value); j++ {
		switch value[j] {
		case '\\':
			j++
		case '"':
			return j + 1
		}
	}
	return -1
}
