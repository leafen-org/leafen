package app

// notionMarkdownUnknownCopyWay is how a page update's refusal of new
// <unknown/> lines tells an agent to make another of a block the Markdown
// can't write, or bring one from another page: the block itself copied or
// moved through the native API, as the app's duplicate and "Move to" do
// (block_move_route.go), rather than handed to the person (2026-11-22, the
// user: everything a person can do, an agent does to the end). Its IDs are
// the ones the block's <unknown> url names, which the route takes with or
// without dashes. No braces: the refusal's template keeps them for its own
// names.
const notionMarkdownUnknownCopyWay = "otherwise copy or move the block itself, as the app's duplicate and “Move to” do: " +
	"leafen_call POST /pages/PAGE/blocks/BLOCK/move, PAGE and BLOCK being the IDs its `<unknown>` url names (…/p/PAGE#BLOCK), " +
	"with a body of requestId (a new ID of yours), destinationPageId (the page it goes on), operation \"copy\" (a duplicate) " +
	"or \"move\" and placeAfter (the block it goes after); then fetch the page again and edit around it."
