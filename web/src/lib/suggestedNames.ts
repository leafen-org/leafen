import { i18next } from "@/i18n";

// A friendly name to start an account with where the person's own isn't
// known (the setup of a server opened in a browser; the issue's name
// default, user 2026-10-20 09:00 "화면 언어의 목록에서 고름"): a gentle word
// and a small animal or leaf, from the screen language's own lists in the
// catalog (authGate:suggestedName — its words, and how the two go together).
// A language whose catalog has no lists yet suggests no name: the field
// stands empty under its own placeholder rather than hold an English name on
// a Japanese screen, a name the person would only have to clear (2026-11-25).
// The person changes it any time.

type NameLists = { first: readonly string[]; second: readonly string[] };

function words(value: unknown): readonly string[] {
  return Array.isArray(value) ? value.filter((word): word is string => typeof word === "string" && word.trim() !== "") : [];
}

/** The language whose own catalog holds name lists: the language itself, or the one it is a variant of; "" for none. */
function namesOf(language: string): string {
  const base = language.split("-")[0] ?? "";
  for (const candidate of [language, base]) {
    if (!candidate) continue;
    const lists = i18next.getResource(candidate, "authGate", "suggestedName") as Partial<Record<keyof NameLists, unknown>> | undefined;
    if (words(lists?.first).length && words(lists?.second).length) return candidate;
  }
  return "";
}

/** A friendly name in the language, picked by the two numbers in [0, 1); "" where it has no lists. */
export function suggestedName(language: string, first = Math.random(), second = Math.random()): string {
  const from = namesOf(language);
  if (!from) return "";
  const lists = i18next.getResource(from, "authGate", "suggestedName") as Partial<Record<keyof NameLists, unknown>>;
  const pick = (list: readonly string[], at: number) => list[Math.min(list.length - 1, Math.floor(at * list.length))]!;
  return i18next.t("authGate:suggestedName.pattern", {
    lng: from, first: pick(words(lists.first), first), second: pick(words(lists.second), second),
  });
}

const TAB_SUGGESTION_KEY = "leafen:v2:suggested-name";

/**
 * A friendly name that stays the same in this tab, so a reload doesn't
 * change the name on screen — one for each language's lists, so the
 * language chosen on the setup's first screen gets its own
 * (suggestedNameIn); "" where the language has none.
 */
export function tabSuggestedName(language: string): string {
  const from = namesOf(language);
  if (!from) return "";
  const key = `${TAB_SUGGESTION_KEY}:${from}`;
  try {
    const kept = window.sessionStorage.getItem(key);
    if (kept) return kept;
    const name = suggestedName(from);
    window.sessionStorage.setItem(key, name);
    return name;
  } catch {
    return suggestedName(from);
  }
}

/** A name still this tab's suggestion in one language becomes its suggestion in another; one typed or given stays. */
export function suggestedNameIn(current: string, before: string, after: string): string {
  return current === tabSuggestedName(before) ? tabSuggestedName(after) : current;
}
