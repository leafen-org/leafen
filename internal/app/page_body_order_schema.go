package app

const (
	pageBodyItemPage  = "page"
	pageBodyItemBlock = "block"
)

// pageBodyPlacement is where a new sub-page goes in its parent's body: at the
// start, at the end, or after a block or another sub-page's block.
type pageBodyPlacement struct {
	Type      string
	AfterKind string
	AfterID   string
}
