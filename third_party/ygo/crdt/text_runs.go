package crdt

// TextRun is a string a V1 update carries into a text, as RewriteTextRunsV1
// shows it: the name of the root text holding it ("" when that isn't known
// or the text isn't a root), the formatting it has where it stands, and its
// characters. (Leafen fork.)
type TextRun struct {
	Root       string
	Attributes map[string]any
	Text       string
}

// RewriteTextRunsV1 returns a V1 update with the characters of each string
// it carries into a text passed through rewrite, which answers the new
// characters and whether to change them. A string keeps its place and its
// length: an answer of another length in UTF-16 units is ignored. The
// structs are decoded without integrating and encoded again, as
// RewriteUpdateV1 does, so their IDs, clocks and lengths are the update's
// own, and the update itself comes back when nothing changed.
//
// Where doc holds a string live, its root and formatting are doc's: the
// formats to its left, as a text's delta reads them. Otherwise — a string
// doc deleted since, or never held — they are the update's: the formats its
// origin chain passes through in the update, inserted with it as a
// formatted insert writes them. (Leafen: a server shows each reader a text's
// mentions as that reader may read them.)
func RewriteTextRunsV1(doc *Doc, update []byte, rewrite func(TextRun) (string, bool)) ([]byte, error) {
	perClient, ds, store, err := buildMergeStore([][]byte{update}, decodeStructsV1)
	if err != nil {
		return nil, err
	}
	stringItems := []*Item{}
	for _, items := range perClient {
		for _, item := range items {
			if _, ok := item.Content.(*ContentString); ok && !item.Deleted {
				stringItems = append(stringItems, item)
			}
		}
	}
	if len(stringItems) == 0 {
		return update, nil
	}
	places := textRunPlaces(doc, stringItems)
	changed := false
	for _, item := range stringItems {
		content := item.Content.(*ContentString)
		place, known := places[item]
		if !known {
			place = textRunFromUpdate(store, item)
		}
		place.Text = content.Str
		next, ok := rewrite(place)
		if !ok || next == content.Str || utf16Len(next) != content.utf16Len {
			continue
		}
		item.Content = NewContentString(next)
		changed = true
	}
	if !changed {
		return update, nil
	}
	return encodeStructStoreV1(perClient, ds, StateVector{}, store), nil
}

// textRunPlaces reads, for the strings doc holds live in a root text, that
// root's name and their formatting, walking each such text once.
func textRunPlaces(doc *Doc, stringItems []*Item) map[*Item]TextRun {
	places := map[*Item]TextRun{}
	if doc == nil {
		return places
	}
	doc.mu.RLock()
	defer doc.mu.RUnlock()
	held := map[*Item][]*Item{}
	texts := map[*abstractType]bool{}
	for _, item := range stringItems {
		live := doc.store.Find(item.ID)
		if live == nil || live.Deleted || live.Parent == nil || live.Parent.item != nil || live.Parent.name == "" {
			continue
		}
		if _, ok := live.Content.(*ContentString); !ok {
			continue
		}
		held[live] = append(held[live], item)
		texts[live.Parent] = true
	}
	for text := range texts {
		attributes := map[string]any{}
		for at := text.start; at != nil; at = at.Right {
			if at.Deleted {
				continue
			}
			if format, ok := at.Content.(*ContentFormat); ok {
				if format.Val == nil {
					delete(attributes, format.Key)
				} else {
					attributes[format.Key] = format.Val
				}
				continue
			}
			for _, item := range held[at] {
				places[item] = TextRun{Root: text.name, Attributes: copyTextRunAttributes(attributes)}
			}
		}
	}
	return places
}

// textRunFromUpdate reads a string's root and formatting from the update
// alone: the formats its origin chain passes through, the nearest of each
// key first.
func textRunFromUpdate(store *StructStore, item *Item) TextRun {
	run := TextRun{Attributes: map[string]any{}}
	if item.Parent != nil && item.Parent.item == nil {
		run.Root = item.Parent.name
	}
	seen := map[string]bool{}
	at := item
	for steps := 0; at.Origin != nil && steps < 64; steps++ {
		left := store.Find(*at.Origin)
		if left == nil || left.Deleted {
			break
		}
		format, ok := left.Content.(*ContentFormat)
		if !ok {
			break
		}
		if !seen[format.Key] {
			seen[format.Key] = true
			if format.Val != nil {
				run.Attributes[format.Key] = format.Val
			}
		}
		at = left
	}
	return run
}

func copyTextRunAttributes(attributes map[string]any) map[string]any {
	copied := make(map[string]any, len(attributes))
	for key, value := range attributes {
		copied[key] = value
	}
	return copied
}
