package app

import "errors"

// The refusals of a page body's Markdown replacement (replacePageMarkdown)
// that the change's request or plan makes.

// pageMarkdownRequestError is a Markdown change the page's current Markdown
// can't take.
type pageMarkdownRequestError struct{ message string }

func (e pageMarkdownRequestError) Error() string { return e.message }

// pageMarkdownPlanError is Markdown that makes no blocks, which the public
// API's update refuses with its tool's data beside the words, as Notion's
// does (seen 2026-11-13: the deletion warning, "Markdown errors blocked
// parsing", and new <unknown/> blocks with their words' template; its page
// creation says none).
type pageMarkdownPlanError struct{ message, template string }

func (e pageMarkdownPlanError) Error() string { return e.message }

func (e pageMarkdownPlanError) data() map[string]any {
	return notionToolErrorData("invalid_input", e.template)
}

// notionMarkdownNewUnknownTemplate is the template of
// notionMarkdownNewUnknownError's words.
const notionMarkdownNewUnknownTemplate = "This page {operation} can't make new `<unknown .../>` blocks: " +
	"`<unknown .../>` stands for a block or content the tool can't fully see." +
	"{updateOnlyContext} Take out the new `<unknown .../>` if it got in by mistake, or use a database template " +
	"if there is one; " + notionMarkdownUnknownCopyWay + "{unknownIdentities}"

// notionMarkdownPlanFailure is a plan's refusal as an update says it.
func notionMarkdownPlanFailure(err error) pageMarkdownPlanError {
	failure := pageMarkdownPlanError{message: err.Error()}
	if unknown := (notionMarkdownNewUnknownError{}); errors.As(err, &unknown) {
		failure.template = notionMarkdownNewUnknownTemplate
	}
	return failure
}
