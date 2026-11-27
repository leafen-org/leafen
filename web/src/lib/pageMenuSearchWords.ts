import { i18next } from "@/i18n";

// The words a page's "…" menu and a database row page's find a few rows by
// beyond their names: Leafen's own, Korean and English. They were written
// from what typing showed in Notion's page "…" and row page "…" menus
// (2026-11-21, a synthetic page and row under a Notion test page: 277 queries
// on the first account's Korean screen and 168 on the second account's English
// one — every row name and its words, 1–3 letter starts in Korean and Latin,
// and the words Notion was seen to find a row by). The fonts and the small
// text are found by "글꼴" and "font"; "f" finds the three fonts alike, ahead
// of rows whose "f" stands inside a word, and "t" finds 목차 ahead of the
// small text, so "font" leads their English words. The version history is
// found by "2" and "2.0"; the updates and analytics by "및", "수정" and
// "edits", not by its own name; the notification choices by "댓글"/"comments"
// and "멘션"/"mentions", not by theirs; the connections by "connections", not
// by "연결". Every other row is found by its name and, on a screen in another
// language, its English name (menuSearchName). Here a row is found by its
// shown name too (withShownName).

/** A row's own words: Korean and English; the English alone where no Korean ones were seen. */
type RowWords = { ko?: string; en: string };

const ROW_WORDS = {
  fontDefault: { ko: "기본 글꼴", en: "Font default" },
  fontSerif: { ko: "세리프 글꼴", en: "Font serif" },
  fontMono: { ko: "모노 글꼴", en: "Font mono" },
  smallText: { ko: "작은 텍스트 글꼴", en: "Font small text" },
  pageHistory: { ko: "버전 기록", en: "Version history 2.0" },
  updatesAndAnalytics: { ko: "업데이트 및 애널리틱스 수정", en: "Updates & analytics edits" },
  allComments: { ko: "댓글", en: "Comments" },
  mentions: { ko: "멘션", en: "Mentions" },
  connections: { en: "Connections" },
} as const satisfies Record<string, RowWords>;

/** A page menu row with words of its own. */
export type PageMenuWordsRow = keyof typeof ROW_WORDS;

/**
 * The words a page menu row is found by: on a Korean screen its Korean words
 * then its English ones, else its English ones — a screen in another language
 * adds the name it shows (menuRowLine).
 */
export function pageMenuSearchWords(row: PageMenuWordsRow): string {
  const { ko, en }: RowWords = ROW_WORDS[row];
  const language = (i18next.resolvedLanguage ?? i18next.language ?? "").toLowerCase();
  return ko && language.startsWith("ko") ? `${ko} ${en}` : en;
}
