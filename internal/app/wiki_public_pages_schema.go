package app

// A published page publishes its wiki's pages as it publishes its sub-pages:
// a page of a wiki joins and leaves the published page graph under the page
// it sits under in the wiki, and moving it within the wiki moves it in the
// graph.

const (
	wikiPublicPagesVersion = 304
	wikiPublicPagesName    = "native_wiki_public_pages"
)
