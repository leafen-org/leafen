import type { SessionUser } from "@/authWire.generated";
import type { Page } from "@/types";
import type { WorkspaceSearchActor, WorkspaceSearchResult } from "@/workspaceSearchWire.generated";
import { LEAFEN_SYSTEM_USER_ID, personName } from "@/lib/peopleDirectory";
import { recentTimeStep, type RecentTimeStep } from "@/lib/recentTime";
import { readSearchKeywords, searchKeywordsHeld, searchTextQuery } from "./searchModel";
import { searchMarkRanges, searchTextHoldsWritten, searchTitleHoldsStart, searchTypedStart } from "./searchMarks";
import { foldSearchText } from "@/lib/textMatch";
import { peopleDatabaseHit } from "./searchPeopleHits";

type Translate = (key: string, options?: Record<string, unknown>) => string;

/** "Edited: 5m ago", as Notion's search results write when a page was last
 * edited, in the steps of its recent times (recentTimeStep: "편집 시간: 1일 전"). */
export function searchEditedTime(value: string, now: number, t: Translate, language: string) {
  const time = Date.parse(value);
  if (Number.isNaN(time)) return "";
  return t("searchDialog:editedAt", { time: searchRecentTime(recentTimeStep(time, now, language), t) });
}

function searchRecentTime(recent: RecentTimeStep, t: Translate) {
  switch (recent.step) {
    case "now": return t("searchDialog:editedTime.now");
    case "minutes": return t("searchDialog:editedTime.minutes", { minutes: recent.count });
    case "hours": return t("searchDialog:editedTime.hours", { hours: recent.count });
    case "days": return t("searchDialog:editedTime.days", { days: recent.count });
    case "date": return recent.date;
  }
}

/**
 * Who last edited a result's page, by the name search gave for them; none if
 * unknown. People's database and its people's pages are the app's own, named
 * "Leafen" as Notion's search names its app "Notion App" there (seen
 * 2026-11-16 in a free workspace): the database whoever's first visit made it.
 */
export function searchEditorName(
  page: Page, actors: ReadonlyMap<string, WorkspaceSearchActor>, reader: SessionUser,
) {
  const editorId = peopleDatabaseHit(page) ? LEAFEN_SYSTEM_USER_ID : page.lastEditedBy || page.createdBy;
  if (!editorId) return "";
  if (editorId === LEAFEN_SYSTEM_USER_ID) return personName(editorId);
  const named = actors.get(editorId)?.displayName.trim();
  if (named) return named;
  return editorId === reader.id ? reader.name.trim() || reader.email : "";
}

/** The line of a result's excerpt that holds the most of its keywords, as
    Notion's rows show the one that matched ("paste line two" over "paste line
    one" for that search, seen 2026-11-07). */
export function searchPreviewLine(preview: string, query: string) {
  const lines = preview.split("\n").map((line) => line.trim()).filter(Boolean);
  const terms = foldSearchText(searchTextQuery(query)).split(" ").filter(Boolean);
  let best = lines[0] ?? "", bestCount = 0;
  for (const line of lines) {
    const folded = foldSearchText(line);
    const count = terms.filter((term) => folded.includes(term)).length;
    if (count > bestCount) { best = line; bestCount = count; }
  }
  return best;
}

/** The keywords a result's line marks, as Notion's rows mark what was found in the page's words: those its title and
    place don't hold or, in a line holding every keyword where they don't hold them all, every one (seen 2026-11-11:
    "vorquen plistar" marks "plistar" alone in "plistar only line four" under "W383 Vorquen four", nothing in
    "vorquen only line two" under "W383 Vorquen two" or a row of the database "W383 Vorquen Plistar fulldb", both in
    "vorquen plistar line five b" under "W383 plain five"; 2026-11-16 in two tabs: "hostrak vimble" both in "hostrak
    vimble line two" under "W1507 hostrak title", "dravik mulpen" nothing in "dravik mulpen both" under "W1507 dravik
    mulpen"); and the last word typed by the letters it starts a word with, where they don't hold it (typedStart,
    searchTypedStart). A title holds a word as written (searchTextHoldsWritten — not by its English stem: "run zzqq"
    marks "{running} x {running} x {running}" under "w2486 lx [runs]", seen 2026-11-24 in two tabs; 2026-11-16 in two
    tabs "pobbler" marked "pobbler trelvan line three" under "W1507 pobblering title") or as a word's start, a place
    where it marks it whole (seen 2026-11-24 in two tabs: "line gri" marks "grindle [line] one" under "w2474 qa
    grindlewick", "grindle line" "[grindle line] one", holding both). A line is held whole only. */
export function searchLineQuery(query: string, line: string, title: string, place = ""): { query: string; typedStart?: string } {
  const keywords = readSearchKeywords(query);
  const phraseIn = (phrase: string[], text: string) => searchKeywordsHeld({ phrases: [phrase], words: [] }, text);
  const wordIn = (word: string, text: string) => searchMarkRanges(text, word).length > 0;
  const phrasesHeld = keywords.phrases.map((phrase) => [title, place].some((text) => phraseIn(phrase, text)));
  const wordsHeld = keywords.words.map((word) => searchTextHoldsWritten(title, word) || searchTitleHoldsStart(title, query, word) || wordIn(word, place));
  const lineHoldsAll = keywords.phrases.every((phrase) => phraseIn(phrase, line)) && keywords.words.every((word) => wordIn(word, line));
  const markAll = lineHoldsAll && [...phrasesHeld, ...wordsHeld].some((held) => !held);
  return {
    query: [
      ...keywords.phrases.filter((_, index) => markAll || !phrasesHeld[index]).map((phrase) => `"${phrase.join(" ")}"`),
      ...keywords.words.filter((_, index) => markAll || !wordsHeld[index]),
    ].join(" "),
    typedStart: searchTypedStart(query, title, place),
  };
}

/** Whether a result's line is words found inside one of its page's files, which
    its row names apart; a line that is a file block's name is the page's own
    line, as Notion's search shows it (seen 2026-11-14: "zq984alpha.txt"). */
export function searchLineInFile(hit: Pick<WorkspaceSearchResult, "file">, line: string) {
  return Boolean(hit.file) && !(line && hit.file && line.trim() === hit.file.name.trim());
}
