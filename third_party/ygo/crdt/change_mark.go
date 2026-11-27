package crdt

// ChangeMark is where a document stood at one moment: its state vector and
// its delete set. EncodeChangesSinceV1 encodes what the document changed
// since, as the merge of its transactions' updates would: the items added
// after the mark and only the deletions made after it. An update encoded
// against the mark's state vector carries the document's whole delete set
// instead (Yjs encodeStateAsUpdate does too), every deletion ever made.
// (Leafen fork.)
type ChangeMark struct {
	sv      StateVector
	deleted DeleteSet
}

// MarkChanges returns where doc stands now.
func MarkChanges(doc *Doc) ChangeMark {
	doc.mu.Lock()
	defer doc.mu.Unlock()
	return ChangeMark{sv: doc.store.StateVector(), deleted: cloneDeleteSet(doc.store.deleteSet())}
}

// StateVector is the document's state vector at the mark.
func (mark ChangeMark) StateVector() StateVector {
	return mark.sv
}

// EncodeChangesSinceV1 encodes what doc changed since mark as a V1 update,
// with the structs and deletions still parked on a missing dependency, as
// EncodeStateAsUpdateV1 has them.
func EncodeChangesSinceV1(doc *Doc, mark ChangeMark) []byte {
	doc.mu.Lock()
	defer doc.mu.Unlock()
	since := deletedSince(doc.store.deleteSet(), mark.deleted)
	return withParked(doc, mark.sv, encodeV1Locked(doc, mark.sv, since), encodeStructStoreV1, MergeUpdatesV1)
}

// deletedSince is the part of current, a store's sorted and compact delete
// set, that earlier, sorted and compact too, does not hold.
func deletedSince(current, earlier DeleteSet) DeleteSet {
	out := newDeleteSet()
	for client, ranges := range current.clients {
		before := earlier.clients[client]
		var kept []DeleteRange
		j := 0
		for _, r := range ranges {
			start, end := r.Clock, r.Clock+r.Len
			for start < end {
				for j < len(before) && before[j].Clock+before[j].Len <= start {
					j++
				}
				if j == len(before) || before[j].Clock >= end {
					kept = append(kept, DeleteRange{Clock: start, Len: end - start})
					break
				}
				if before[j].Clock > start {
					kept = append(kept, DeleteRange{Clock: start, Len: before[j].Clock - start})
				}
				start = before[j].Clock + before[j].Len
			}
		}
		if len(kept) > 0 {
			out.clients[client] = kept
		}
	}
	return out
}
