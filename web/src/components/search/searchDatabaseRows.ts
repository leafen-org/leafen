import { foldSearchText } from "@/lib/textMatch";
import {
  readSearchKeywords,
  searchHitRows,
  searchKeywordsHeld,
  searchKeywordWord,
  searchTextQuery,
  titleAnswerScore,
  type SearchHit,
  type SearchKeywords,
} from "./searchModel";

/** Whether a text holds some of the keywords: a phrase whole, or a word as a title holds it. */
function someKeywordsHeld(keywords: SearchKeywords, text: string) {
  return keywords.phrases.some((phrase) => searchKeywordsHeld({ phrases: [phrase], words: [] }, text))
    || keywords.words.some((_, index) => searchKeywordsHeld(searchKeywordWord(keywords, index), text));
}

type Rows = { found: SearchHit[]; byPath: SearchHit[] };

/**
 * A database of several sources' rows for the keywords: its own name's row
 * and its sources', those found by every keyword when any is, else those
 * found by some; null when no name of it holds any.
 */
function databaseRows(hit: SearchHit, keywords: SearchKeywords, query: string): (Rows & { every: boolean }) | null {
  const sources = hit.database?.sources ?? [];
  const ownName = hit.database?.named ? foldSearchText(hit.page.title.trim()) : "";
  const names = sources.map((source) => foldSearchText(source.title.trim()));
  const read = (holds: (text: string) => boolean, every: boolean): Rows => {
    const found: { row: SearchHit; score: number; at: number }[] = [];
    const byPath: SearchHit[] = [];
    if (ownName && holds(ownName)) found.push({ row: hit, score: titleAnswerScore(ownName, query), at: -1 });
    sources.forEach((source, at) => {
      const name = names[at];
      if (!name) return;
      // A name holding some of the keywords, the database's own name the rest, is found by both.
      if (holds(name) || (every && ownName && someKeywordsHeld(keywords, name) && holds(`${ownName} ${name}`))) {
        found.push({ row: { ...hit, source }, score: titleAnswerScore(name, query), at });
      } else if (every && ownName && holds(ownName)) byPath.push({ ...hit, source });
    });
    found.sort((left, right) => left.score - right.score || left.at - right.at);
    return { found: found.map(({ row }) => row), byPath };
  };
  const every = read((text) => searchKeywordsHeld(keywords, text), true);
  if (every.found.length || every.byPath.length) return { ...every, every: true };
  const some = read((text) => someKeywordsHeld(keywords, text), false);
  return some.found.length ? { ...some, every: false } : null;
}

/**
 * The rows the results show, a database of several sources found by keywords
 * listed as Notion's search lists it (seen 2026-11-18 in two tabs, four
 * searches: "Kvx", "Kvx 묶음", "다른 원본" and "Burst N"):
 *
 * - by its own name when it has one holding the keywords;
 * - by each source whose name holds them, or holds some and the database's
 *   own name the rest ("Kvx 묶음 / Kvx 하나" for "Kvx 묶음"); these rows
 *   stand where the database was found, the one whose name answers the
 *   words best first, the database's own before its sources' on a tie
 *   ("Kvx 묶음" before "Kvx 묶음 / Kvx 하나" for "Kvx");
 * - by a named source whose name holds none of them only when the database's
 *   own name holds them all, as a page under a page so named is found by its
 *   path: after the results found by their titles or bodies ("Kvx 묶음 /
 *   다른 원본" last for "Kvx"), before those holding only some of the
 *   keywords, as every result holding them all comes first;
 * - never by a source without a name ("새 데이터 소스"), as Notion finds no
 *   untitled page by the name it shows, nor by a source of a database
 *   without a name of its own that its name doesn't find ("남 원본" for
 *   "Kvx", under "Kvx 둘, 새 데이터 소스, 남 원본").
 *
 * A database found by some of the keywords lists the names holding some. One
 * none of whose names holds any (found by its sources' mentions) lists its
 * named sources as before; any other result is one row (searchHitRows), as
 * is every result while browsing without keywords.
 */
export function searchResultRows(hits: readonly SearchHit[], query: string): SearchHit[] {
  const keywords = readSearchKeywords(query);
  if (!keywords.phrases.length && !keywords.words.length) return hits.flatMap(searchHitRows);
  const folded = foldSearchText(searchTextQuery(query));
  const rows: SearchHit[] = [];
  // Rows found by their database's own name alone, which follow the results holding every keyword.
  let byPath: SearchHit[] = [];
  const placePath = () => { rows.push(...byPath); byPath = []; };
  for (const hit of hits) {
    if ((hit.database?.sources?.length ?? 0) < 2) {
      if (hit.match === "title" && !searchKeywordsHeld(keywords, foldSearchText(hit.page.title.trim()))) placePath();
      rows.push(hit);
      continue;
    }
    const found = databaseRows(hit, keywords, folded);
    if (!found) {
      const named = searchHitRows(hit).filter((row) => !row.source || row.source.title.trim());
      rows.push(...(named.length ? named : searchHitRows(hit)));
      continue;
    }
    if (!found.every) placePath();
    rows.push(...found.found);
    byPath.push(...found.byPath);
  }
  placePath();
  return rows;
}
