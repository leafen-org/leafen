import { useLayoutEffect } from "react";
import { pagePeekHref } from "@/lib/pagePeek";
import { personProfileHref } from "@/lib/peopleProfiles";
import { replaceRoute } from "@/lib/router";

/**
 * A People row's page is its person's profile, as Notion's person page is
 * one page — "People / 〈이름〉", their cover, picture, activity and pages
 * (seen 2026-11-15 opening a person mention) — whatever opens it. Where a
 * row's page would show one (`person`, the server's mark of a People row) —
 * a favourite, a relation, a button's "열기", its link — the profile opens
 * in its place: full where the row would open full, else in the same peek —
 * before the row's page is painted, which flashed in its place first
 * (2026-11-15, a favourite opening a person).
 */
export function usePersonRowPage(person: string | undefined, mode: "side" | "center" | "full") {
  useLayoutEffect(() => {
    if (person) replaceRoute(mode === "full" ? personProfileHref(person) : pagePeekHref(person, mode));
  }, [mode, person]);
}
