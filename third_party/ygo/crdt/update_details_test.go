package crdt

import "testing"

func TestOnUpdateDetailsNamesChangedText(t *testing.T) {
	doc := New()
	var observed UpdateDetails
	unsubscribe := doc.OnUpdateDetails(func(details UpdateDetails) {
		observed = details
	})
	defer unsubscribe()

	doc.Transact(func(txn *Transaction) {
		txn.GetText("leafen:block-text:block-b").Insert(txn, 0, "world", nil)
		txn.GetText("leafen:block-text:block-a").Insert(txn, 0, "hello", nil)
	})
	if len(observed.Update) == 0 || len(observed.TextChanges) != 2 {
		t.Fatalf("unexpected update details: update=%d changes=%d", len(observed.Update), len(observed.TextChanges))
	}
	if observed.TextChanges[0].Name != "leafen:block-text:block-a" ||
		observed.TextChanges[1].Name != "leafen:block-text:block-b" {
		t.Fatalf("unexpected text changes: %+v", observed.TextChanges)
	}
}
