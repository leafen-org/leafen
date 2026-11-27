package crdt

import "sort"

// UpdateRootsV1 names the root types a V1 update writes into: the root of
// each struct it carries, where the update's own structs place it, or else
// where doc holds the item it follows or precedes. The structs are decoded
// without integrating, so a struct doc can't integrate yet — one whose
// same-client predecessors haven't reached doc — is named as well. A struct
// whose place neither tells is left out, as is one inside a nested type.
// Deletions aren't read: applied to doc, they name their roots as changes.
// (Leafen fork: a page's history names each block whose text a stored update
// writes, also an update stored ahead of the text it builds on.)
func UpdateRootsV1(doc *Doc, update []byte) ([]string, error) {
	perClient, _, _, err := buildMergeStore([][]byte{update}, decodeStructsV1)
	if err != nil {
		return nil, err
	}
	named := map[string]bool{}
	name := func(parent *abstractType) {
		if parent != nil && parent.item == nil && parent.name != "" {
			named[parent.name] = true
		}
	}
	unplaced := []*Item{}
	for _, items := range perClient {
		for _, item := range items {
			switch {
			case item.Parent != nil:
				name(item.Parent)
			case item.Origin != nil || item.OriginRight != nil:
				unplaced = append(unplaced, item)
			}
		}
	}
	if doc != nil && len(unplaced) > 0 {
		doc.mu.RLock()
		for _, item := range unplaced {
			var held *Item
			if item.Origin != nil {
				held = doc.store.Find(*item.Origin)
			}
			if held == nil && item.OriginRight != nil {
				held = doc.store.Find(*item.OriginRight)
			}
			if held != nil {
				name(held.Parent)
			}
		}
		doc.mu.RUnlock()
	}
	roots := make([]string, 0, len(named))
	for root := range named {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots, nil
}
