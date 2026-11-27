package crdt

// ChangedRoots names the root types this transaction changed, in themselves
// or in a type nested in them, in no set order: the roots of Yjs's public
// transaction.changed. Read from an OnAfterTransaction callback, it tells
// which of a document's many root types an applied update touched, its
// deletions included, which UpdateRootsV1 leaves out. (Leafen fork: a page's
// history, planning a revert, reads again only the blocks each replayed
// version changed.)
func (txn *Transaction) ChangedRoots() []string {
	named := make(map[string]struct{}, len(txn.changed))
	for changed := range txn.changed {
		root := changed
		for root != nil && root.item != nil {
			root = root.item.Parent
		}
		if root != nil && root.name != "" {
			named[root.name] = struct{}{}
		}
	}
	roots := make([]string, 0, len(named))
	for name := range named {
		roots = append(roots, name)
	}
	return roots
}
