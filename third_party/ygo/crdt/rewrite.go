package crdt

// RewriteUpdateV1 returns a V1 update with every string value it carries — in
// ContentAny, ContentJSON, ContentEmbed and ContentFormat values, nested maps
// and arrays included, and map entries' keys (ParentSub) — passed through
// rewrite, which answers the new string and whether to change it. Strings in
// a text (ContentString) and format keys stay. The structs are decoded
// without integrating and encoded again, so their IDs, clocks and lengths are
// the update's own (Leafen: a workspace moved to another server names the
// accounts there its people were paired with, inside its pages' history).
func RewriteUpdateV1(update []byte, rewrite func(string) (string, bool)) ([]byte, error) {
	perClient, ds, store, err := buildMergeStore([][]byte{update}, decodeStructsV1)
	if err != nil {
		return nil, err
	}
	for _, items := range perClient {
		for _, item := range items {
			if item.ParentSub != nil {
				if next, ok := rewrite(*item.ParentSub); ok {
					item.ParentSub = &next
				}
			}
			switch content := item.Content.(type) {
			case *ContentAny:
				for index, value := range content.Vals {
					content.Vals[index] = rewriteValue(value, rewrite)
				}
			case *ContentJSON:
				for index, value := range content.Vals {
					content.Vals[index] = rewriteValue(value, rewrite)
				}
			case *ContentEmbed:
				content.Val = rewriteValue(content.Val, rewrite)
			case *ContentFormat:
				content.Val = rewriteValue(content.Val, rewrite)
			}
		}
	}
	return encodeStructStoreV1(perClient, ds, StateVector{}, store), nil
}

func rewriteValue(value any, rewrite func(string) (string, bool)) any {
	switch typed := value.(type) {
	case string:
		if next, ok := rewrite(typed); ok {
			return next
		}
	case []any:
		for index, item := range typed {
			typed[index] = rewriteValue(item, rewrite)
		}
	case map[string]any:
		renamed := map[string]any{}
		for key, item := range typed {
			if next, ok := rewrite(key); ok {
				delete(typed, key)
				renamed[next] = rewriteValue(item, rewrite)
				continue
			}
			typed[key] = rewriteValue(item, rewrite)
		}
		for key, item := range renamed {
			typed[key] = item
		}
	}
	return value
}
