package app

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"unicode/utf8"

	"github.com/reearth/ygo/crdt"
)

// A block's metadata in a structure document is read as JSON reads it —
// numbers as float64, bytes as base64 text — without going through JSON:
// every block's metadata read that way, a few times a change, took a large
// part of each turn of a long page's import (2026-10-15). A value this
// doesn't know goes through JSON instead.

// structuralMetadataFields reads one block's metadata, or ok false when it
// should go through JSON.
func structuralMetadataFields(value any) (parentID, blockType string, content map[string]any, ok bool) {
	fields, isMap := value.(map[string]any)
	if !isMap || len(fields) > 3 {
		return "", "", nil, false
	}
	for key, field := range fields {
		switch key {
		case "parentId", "type":
			text, isText := field.(string)
			if field != nil && (!isText || !utf8.ValidString(text)) {
				return "", "", nil, false
			}
			if key == "parentId" {
				parentID = text
			} else {
				blockType = text
			}
		case "content":
			if field == nil {
				continue
			}
			object, isObject := field.(map[string]any)
			if !isObject {
				return "", "", nil, false
			}
			shaped, known := jsonShapedValue(object)
			if !known {
				return "", "", nil, false
			}
			content, _ = shaped.(map[string]any)
		default:
			return "", "", nil, false
		}
	}
	return parentID, blockType, content, true
}

// jsonShapedValue is what a value becomes through JSON, or ok false for a
// value it doesn't know.
func jsonShapedValue(value any) (any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, true
	case string:
		return typed, utf8.ValidString(typed)
	case bool:
		return typed, true
	case float64:
		return typed, !math.IsNaN(typed) && !math.IsInf(typed, 0)
	case int64:
		return float64(typed), true
	case int:
		return float64(typed), true
	case []byte:
		if typed == nil {
			return nil, true
		}
		return base64.StdEncoding.EncodeToString(typed), true
	case map[string]any:
		if typed == nil {
			return nil, true
		}
		shaped := make(map[string]any, len(typed))
		for key, item := range typed {
			if !utf8.ValidString(key) {
				return nil, false
			}
			next, ok := jsonShapedValue(item)
			if !ok {
				return nil, false
			}
			shaped[key] = next
		}
		return shaped, true
	case []any:
		if typed == nil {
			return nil, true
		}
		shaped := make([]any, len(typed))
		for index, item := range typed {
			next, ok := jsonShapedValue(item)
			if !ok {
				return nil, false
			}
			shaped[index] = next
		}
		return shaped, true
	}
	return nil, false
}

// structuralMetadataView reads blocks' metadata from a structure document a
// block at a time, and all of it only when asked — a deletion takes a block's
// whole subtree — so a long page's change touching a few blocks reads a few
// (2026-10-15: every block's read twice a turn of a long page's import, and
// left for the collector). Changes made to the document are told to it too.
type structuralMetadataView struct {
	doc     *crdt.Doc
	entries map[string]structuralBlockMetadata
	absent  map[string]struct{}
	whole   bool
}

func newStructuralMetadataView(doc *crdt.Doc) *structuralMetadataView {
	return &structuralMetadataView{doc: doc, entries: map[string]structuralBlockMetadata{}, absent: map[string]struct{}{}}
}

func (view *structuralMetadataView) get(id string) (structuralBlockMetadata, bool, error) {
	if entry, known := view.entries[id]; known {
		return entry, true, nil
	}
	if _, gone := view.absent[id]; gone || view.whole {
		return structuralBlockMetadata{}, false, nil
	}
	value, found := view.doc.GetMap(ygoBlockMetadataRoot).Get(id)
	if !found {
		view.absent[id] = struct{}{}
		return structuralBlockMetadata{}, false, nil
	}
	entry, err := structuralMetadataEntry(value)
	if err != nil {
		return structuralBlockMetadata{}, false, err
	}
	view.entries[id] = entry
	return entry, true, nil
}

func (view *structuralMetadataView) set(id string, entry structuralBlockMetadata) {
	view.entries[id] = entry
	delete(view.absent, id)
}

func (view *structuralMetadataView) remove(id string) {
	delete(view.entries, id)
	view.absent[id] = struct{}{}
}

// all is every block's metadata, the view's own from then on.
func (view *structuralMetadataView) all() (map[string]structuralBlockMetadata, error) {
	if !view.whole {
		entries, err := structuralMetadata(view.doc)
		if err != nil {
			return nil, err
		}
		view.entries, view.absent, view.whole = entries, map[string]struct{}{}, true
	}
	return view.entries, nil
}

// structuralMetadataEntry reads one block's metadata as JSON does.
func structuralMetadataEntry(value any) (structuralBlockMetadata, error) {
	if parentID, blockType, content, ok := structuralMetadataFields(value); ok {
		if blockType == "" || content == nil {
			return structuralBlockMetadata{}, graphStructureAuthorityError()
		}
		return structuralBlockMetadata{ParentID: parentID, Type: blockType, Content: content}, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return structuralBlockMetadata{}, err
	}
	var stored struct {
		ParentID string         `json:"parentId"`
		Type     string         `json:"type"`
		Content  map[string]any `json:"content"`
	}
	if json.Unmarshal(encoded, &stored) != nil || stored.Type == "" || stored.Content == nil {
		return structuralBlockMetadata{}, graphStructureAuthorityError()
	}
	return structuralBlockMetadata{ParentID: stored.ParentID, Type: stored.Type, Content: stored.Content}, nil
}
