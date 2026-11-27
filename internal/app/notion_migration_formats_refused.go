package app

import (
	"fmt"
	"strings"
)

// A reading past Leafen's size or count for one reading is refused where it
// lands — in the person's Notion tab, saying why, the limit and what of it
// was how big — and kept as refused on the reader's row
// (takeNotionMigrationFormats), so the agent's next
// leafen_notion_migration_formats says so instead of asking for the reader
// again; the run goes on without it and lists what it couldn't match. Only
// its settings coming whole are needed: a reading whose rows' orders alone
// didn't fit is taken without them (notion_migration_formats_parts.go), and
// says so the same ways.

const (
	notionMigrationFormatsRefusedSize  = "too_big"
	notionMigrationFormatsRefusedCount = "too_many"
)

// notionMigrationFormatsRefusal is why a reading is refused, or "".
func notionMigrationFormatsRefusal(tooBig bool, read notionMigrationFormatsRead) string {
	if tooBig {
		return notionMigrationFormatsRefusedSize
	}
	if len(read.Pages)+len(read.Databases)+len(read.Views)+len(read.Unread) > notionMigrationFormatsMostItems {
		return notionMigrationFormatsRefusedCount
	}
	return ""
}

func notionMigrationFormatsRefusedWords(refused string, notTaken []notionMigrationFormatsPart) string {
	if refused == notionMigrationFormatsRefusedCount {
		return fmt.Sprintf("it held more than %d pages, databases, views and items it couldn't read, more than Leafen takes in one reading.",
			notionMigrationFormatsMostItems)
	}
	return fmt.Sprintf("it was over %d MiB, more than Leafen takes in one reading", notionMigrationFormatsMaxBytes>>20) +
		notionMigrationFormatsPartsWords(notTaken, 0) + "."
}

// notionMigrationFormatsPartsWords says what the parts not taken were and
// how big as sent, and unpacked when packed: " (the settings 18.2 MiB, 40.1
// MiB unpacked; …)". A part too small to tell goes unsaid.
func notionMigrationFormatsPartsWords(parts []notionMigrationFormatsPart, language int) string {
	names := [2]map[string]string{{"discussions": "which blocks have comments", "formats": "the settings", "orders": "the rows’ orders"},
		{"discussions": "댓글 있는 블록", "formats": "꾸밈", "orders": "행 순서"}}
	said := []string{}
	for _, part := range parts {
		name, known := names[language][part.Name]
		if !known || part.Sent < 1<<17 {
			continue
		}
		words := name + " " + notionMigrationFormatsSizeWords(part.Sent)
		if part.Read > part.Sent {
			words += [2]string{", %s unpacked", ", 압축 전 %s"}[language]
			words = fmt.Sprintf(words, notionMigrationFormatsSizeWords(part.Read))
		}
		said = append(said, words)
	}
	if len(said) == 0 {
		return ""
	}
	return [2]string{" (", "("}[language] + strings.Join(said, "; ") + ")"
}

// notionMigrationFormatsRefusedPage says in the person's tab why Leafen
// didn't take the reading (takeNotionMigrationFormats kept why for the agent).
func notionMigrationFormatsRefusedPage(refused string, notTaken []notionMigrationFormatsPart, comments bool) notionMigrationFormatsWords {
	why := notionMigrationFormatsWords{notionMigrationFormatsRefusedWords(refused, notTaken),
		fmt.Sprintf("한 번에 받는 크기(%d MiB)를 넘었습니다%s.", notionMigrationFormatsMaxBytes>>20, notionMigrationFormatsPartsWords(notTaken, 1))}
	if refused == notionMigrationFormatsRefusedCount {
		why[1] = fmt.Sprintf("페이지·데이터베이스·보기와 읽지 못한 항목이 한 번에 받는 개수(%d개)를 넘었습니다.", notionMigrationFormatsMostItems)
	}
	words := notionMigrationFormatsWords{
		"Leafen didn’t take what was read: " + why[0] + " The move goes on without it and lists what it couldn’t match.",
		"리펜이 읽은 것을 받지 않았습니다. " + why[1] + " 옮기기는 이것 없이 이어지고, 맞추지 못한 것을 하나씩 알려 줍니다."}
	if comments {
		words[0] += " " + notionMigrationFormatsCommentsKeptWords
		words[1] += " 댓글이 있는 블록은 받았으니 댓글 옮기기는 느려지지 않습니다."
	}
	return words
}

// notionMigrationFormatsCommentsKeptWords says, in English, that of a reading
// refused Leafen kept which blocks have comments (notion_migration_formats_parts.go).
const notionMigrationFormatsCommentsKeptWords = "It kept which blocks have comments, so the comments step still asks only those."

// notionMigrationFormatsTakenPage says in the person's tab that Leafen took
// the reading, and what of it it didn't.
func notionMigrationFormatsTakenPage(notTaken []notionMigrationFormatsPart) notionMigrationFormatsWords {
	if len(notTaken) == 0 {
		return notionMigrationFormatsTakenWords
	}
	return notionMigrationFormatsWords{
		notionMigrationFormatsTakenWords[0] + " " + notionMigrationFormatsNotTakenWords(notTaken),
		notionMigrationFormatsTakenWords[1] + fmt.Sprintf(" 다만 행 순서는 한 번에 받는 크기(%d MiB)를 넘어 받지 않았습니다%s. 손으로 순서를 정한 보기는 리펜의 순서로 두고, 맞추지 못한 것과 함께 알려 줍니다.",
			notionMigrationFormatsMaxBytes>>20, notionMigrationFormatsPartsWords(notTaken, 1))}
}

// notionMigrationFormatsNotTakenWords says, in English, what of a reading
// taken Leafen didn't take.
func notionMigrationFormatsNotTakenWords(notTaken []notionMigrationFormatsPart) string {
	return fmt.Sprintf("It didn’t take the rows’ orders, over the %d MiB it takes in one reading%s: views put in order by hand keep Leafen’s order and are listed with what it couldn’t match.",
		notionMigrationFormatsMaxBytes>>20, notionMigrationFormatsPartsWords(notTaken, 0))
}
