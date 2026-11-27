package app

// A wiki page's verification is the value of its wiki's Verification
// property, as Notion's is, and its owners the values of the wiki's Owner
// property. A wiki's data source names those two properties. The page record
// follows the verification value, as its title follows the title value, so
// search, mentions and the sidebar read it as any page's: each new value
// starts a new verification generation, cancels what the last one had
// scheduled and, when it runs until a time, schedules the reminder to its
// owners for that time.

const (
	wikiVerificationValuesVersion = 303
	wikiVerificationValuesName    = "native_wiki_verification_values"
)
