package app

import "time"

// notionMigrationFormatsToolOutput is what leafen_notion_migration_formats
// answers: the reader with its key (notion_migration_formats_key.go), the
// reading Leafen has, and what the agent does next. Checking whether the
// reader has read is a call like any other: until a reading comes in, it
// gives the same reader and says the one already given may still be reading.
func notionMigrationFormatsToolOutput(
	script string, given notionMigrationFormatsKey, reader notionMigrationFormatsReader, location *time.Location,
) leafenMCPNotionMigrationFormatsOutput {
	output := leafenMCPNotionMigrationFormatsOutput{Reader: script, ReaderGivenAt: personClockStamp(given.GivenAt, location)}
	if reader.ReadAt > 0 {
		counts := reader.Counts
		output.Read, output.ReadAt = &counts, personClockStamp(time.UnixMilli(reader.ReadAt), location)
	}
	stillReading := "With many rows it reads for minutes, and longer when Notion asks it to slow down; " +
		"calling this tool doesn't stop it, and answers with read once the reading is in. Leafen's import window (link) shows the reading live, with its counts. " +
		"If the tab was closed or left the page before it went to Leafen and back, the reading runs again from reader_page."
	switch {
	case reader.Refused != "":
		output.Refused = notionMigrationFormatsRefusedWords(reader.Refused, reader.NotTaken)
		again := " Running `reader` again sends the same; the run goes on without it and lists what it couldn't match."
		if reader.Refused == notionMigrationFormatsRefusedSize && len(reader.NotTaken) == 0 {
			// A reader from before it sent the same settings once (notion_migration_formats_parts.go).
			again = " That reader sent the same settings over and over; this `reader` sends them once, packed: run from reader_page as the first time " +
				"(reader_start), it hands in, and this tool answers with read."
		}
		if reader.RefusedAt > 0 && reader.Discussions != nil {
			output.Refused += " " + notionMigrationFormatsCommentsKeptWords
		}
		output.Next = "Leafen refused the last reading the reader sent (refused): " + output.Refused + again
	case reader.ReadAt == 0 && given.Again:
		output.Next = "No reading has come in yet. `reader` is the one Leafen gave at reader_given_at: if it is still reading in the Notion tab, " +
			"it goes on there. " + stillReading
	case reader.ReadAt == 0:
		output.Next = "No reading yet: the settings reading the one question explained goes in the person's Notion. " + notionMigrationLiveWords + ". " +
			"reader_page is Leafen's page for it, opened in the browser the token came from, theirs or the agent's own; its one button opens Notion in a new tab (they log in once if it asks). " +
			"In that Notion tab, reader_start — a line, with a browser tool's JavaScript — takes the reader from Leafen's page and runs it (`reader` is the same reader whole, " +
			"for a tab opened otherwise); it tells Leafen how far it is whatever Leafen's address, and answers once the tab has gone to Leafen and back, " +
			"and this tool then answers with read — the same reader until then. " +
			stillReading + " Without a browser to run it in, the run goes on without it and lists what it couldn't match."
	default:
		output.Next = "Leafen has a reading (read): the run matches it, or matched it already when the run was done and wrote its report again. " +
			"Reading again is for Notion's pages or views changed since."
		if given.Again {
			output.Next += " A reader run again since takes this one's place when it hands in: read_at is then after it ran. " + stillReading
		}
		if len(reader.NotTaken) > 0 {
			output.Next += " Leafen took the reading but not all of it: " + notionMigrationFormatsNotTakenWords(reader.NotTaken)
		}
		if reader.Counts.Unread > 0 {
			output.Next += " The reader couldn't read some (read.unread): the run lists them with what it couldn't match."
		}
	}
	return output
}
