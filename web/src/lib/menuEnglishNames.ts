import { i18next } from "@/i18n";
import { withShownName } from "./menuSearch";

// A page menu row's English name, for a search typed in English on a screen
// in another language: Notion's page "…" finds its rows by their English
// names too (seen 2026-11-15 and 2026-11-16 in Korean: "ab" 목차 — "Table of
// contents" — and on a database 데이터베이스 잠금, "move" 옮기기 and 휴지통으로
// 이동, "his" 버전 기록, "dup" 복제; "del" nothing, so no other words). A row's
// name is looked up among the menu's catalogs, each line shown → its English.

const MENU_NAMESPACES = ["rowMenu", "topBar"] as const;

type Names = Map<string, string[]>;
let built: { language: string; english: number; names: Names } | null = null;

function lines(bundle: unknown, path: string, into: Map<string, string>) {
  if (typeof bundle === "string") {
    into.set(path, bundle);
    return;
  }
  if (!bundle || typeof bundle !== "object") return;
  for (const [key, value] of Object.entries(bundle)) lines(value, path ? `${path}.${key}` : key, into);
}

function bundleLines(language: string, namespace: string) {
  const into = new Map<string, string>();
  lines(i18next.getResourceBundle(language, namespace), "", into);
  return into;
}

// English's namespaces stand empty until its words arrive (i18n/index.ts).
function englishLoaded() {
  return MENU_NAMESPACES.reduce((count, namespace) => count + Object.keys(i18next.getResourceBundle("en", namespace) ?? {}).length, 0);
}

function namesNow(): Names | null {
  const language = i18next.language;
  if (!language || language.toLowerCase().startsWith("en")) return null;
  const english = englishLoaded();
  if (built?.language === language && built.english === english) return built.names;
  const names: Names = new Map();
  for (const namespace of MENU_NAMESPACES) {
    const shown = bundleLines(language, namespace), source = bundleLines("en", namespace);
    for (const [path, line] of shown) {
      const name = source.get(path);
      // A line with a value in it is no row's whole name.
      if (!name || name === line || line.includes("{{")) continue;
      const known = names.get(line);
      if (!known) names.set(line, [name]);
      else if (!known.includes(name)) known.push(name);
    }
  }
  built = { language, english, names };
  return names;
}

/** The English names a menu row shown as `label` has, none on an English screen or before English's words arrive. */
export function menuEnglishNames(label: string): readonly string[] {
  return namesNow()?.get(label) ?? [];
}

/**
 * The words a menu row (or a list's name) shown as `label` is found by, as
 * Notion's action menus name it for their search (read 2026-11-16 from
 * Notion's web app): its name as shown, then its English name on a screen in
 * another language where that differs ("링크 복사 Copy link"; seen
 * 2026-11-21 in its page "…" on a Korean screen: "link" finds 링크 복사,
 * "appearance" every row under 테마와 레이아웃, "discussions" 페이지 댓글).
 */
export function menuSearchName(label: string): string {
  return [label, ...menuEnglishNames(label).filter((name) => name !== label)].join(" ");
}

/**
 * The words a menu row (or a list's name) is found by: its own words where it
 * has them (`search`, pageMenuSearchWords), else its name's (menuSearchName).
 */
export function menuRowSearchWords(label: string, search?: string): string {
  return search ?? menuSearchName(label);
}

/**
 * The line a menu row is found by (actionMenuScore): its words
 * (menuRowSearchWords), the words of the lists it stands in (`lists`) and its
 * words again, as Notion's action menus read theirs, with its shown name at
 * the end, parted from them, where its words don't hold it (withShownName):
 * Notion finds its 연결 only by "Connections", and here "연" and "연결" find
 * it as well; but a search's letters don't run on from its words
 * into that name — "comments 모든" finds 모든 댓글 in neither (seen
 * 2026-11-22 in Notion's page "…" on a Korean screen).
 */
export function menuRowLine(label: string, search: string | undefined, lists: string): string {
  const words = menuRowSearchWords(label, search);
  return withShownName(`${words} ${lists} ${words}`, label);
}
