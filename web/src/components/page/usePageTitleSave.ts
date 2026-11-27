import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { createPageTitleFollow, followServerPageTitle, serverPageTitle, type PageTitleBase } from "@/lib/pageTitleFollow";
import { sameTitle } from "@/lib/titleMerge";
import { followPageName, typedPageTitle } from "@/lib/pageNames";
import type { TextSpan } from "@/lib/types";
import type { Page } from "@/types";
import type { PageTitleEngine as TitleEngine } from "./pageTitleEngine";
import { imeComposing } from "@/lib/fieldKeyBoundary";
import { holdTypedPageTitle } from "@/lib/pageKindChange";
type TitleFollow = ReturnType<typeof createPageTitleFollow>;

// How long after typing stops a typed title is saved.
const TYPED_TITLE_SAVE_MS = 600;

/**
 * A page title's saving (PageHeader): what is typed is saved shortly after
 * typing stops, and at once when the page is left, the window is hidden (an
 * app going to the background) or the page is unloaded, so it survives the
 * app being ended mid-word, and before a write that changes what the page is
 * (lib/pageKindChange.ts); leaving a page saves to the page left. The field
 * follows the titles saved elsewhere as they come (pageTitleFollow.ts).
 */
export function usePageTitleSave({ page, readOnly, engine, onTitleChange, onTitleTyped }: {
  page: Page;
  readOnly: boolean;
  /** The title's field, made before this hook's effects run. */
  engine: RefObject<TitleEngine | null>;
  /**
   * Saves the title with its spans — none for plain words — and the title it
   * was typed on; answers with the page as saved.
   */
  onTitleChange: (title: string, base?: PageTitleBase, spans?: TextSpan[]) => unknown;
  onTitleTyped?: (title: string) => void;
}) {
  const latest = useRef({ page, onTitleTyped });
  latest.current = { page, onTitleTyped };
  const state = useRef<{ timer: number | null; save: () => void; held: { core: TitleEngine; follow: TitleFollow } | null }>({
    timer: null, save: () => undefined, held: null,
  });
  const [saving] = useState(() => {
    const saveSoon = () => {
      const current = state.current;
      if (current.timer !== null) window.clearTimeout(current.timer);
      current.timer = window.setTimeout(() => current.save(), TYPED_TITLE_SAVE_MS);
    };
    // The follow of the field as it is: one for each field the page makes.
    const follow = (): TitleFollow | null => {
      const core = engine.current, current = state.current;
      if (!core) return null;
      if (current.held?.core !== core) {
        const { page: made } = latest.current;
        current.held = { core, follow: createPageTitleFollow(pageTitleBase(made), {
          read: () => core.value(), composing: () => imeComposing(core.editor.view), focused: () => core.editor.isFocused,
          show: (value) => core.follow(value),
          // A title from elsewhere merged with typing not saved yet: shown as typed, saved as typing is.
          unsaved: (value) => { latest.current.onTitleTyped?.(value.title); saveSoon(); },
        }) };
      }
      return current.held.follow;
    };
    return {
      follow,
      /** Saves what is new in the title now. */
      save: () => state.current.save(),
      /** The title was typed in: saved after the pause, and what waited for a composition to end comes. */
      typed: () => { saveSoon(); follow()?.resume(); },
    };
  });

  useLayoutEffect(() => {
    const follow = saving.follow();
    if (!follow) return;
    const shown = pageTitleBase(page);
    // A title nobody may write here is the page's as it stands; one open to
    // writing takes the server's titles as they come (below).
    if (readOnly) follow.reset(shown);
    else follow.shown(shown);
  }, [saving, page.title, page.titleSpans, page.cursor, readOnly]);
  // A title being typed for the page elsewhere — its sidebar row's or a
  // favorite's rename popover — shows in the field at each key, as the row,
  // the path and the window's title show it, without drawing the page again
  // (lib/pageNames.ts), and goes back if the save is refused; the
  // field's own typing is already in it (shown passes over a field typed in
  // or saving). Notion's page title follows its sidebar row's rename at each
  // key, with the path, 22–32 ms after it (Chrome, 2026-11-18: timed in one
  // tab, seen in a screenshot right after one key in another), and its
  // path piece's rename likewise. A title holding mentions or formatting
  // follows once saved, as before: a name typed is plain words.
  useLayoutEffect(() => followPageName(page.id, () => {
    const { page: now } = latest.current;
    const typed = typedPageTitle(now.id, now.title);
    const base = pageTitleBase(now);
    if (readOnly || !sameTitle({ title: now.title, spans: now.titleSpans }, { title: now.title })) return;
    saving.follow()?.shown(typed === undefined ? base : { title: typed, cursor: base.cursor });
  }), [saving, page.id, readOnly]);
  // A title saved elsewhere — this person's other window, another person —
  // comes into the field as the server answers it, caret in it or not.
  useLayoutEffect(() => {
    const known = serverPageTitle(page.id);
    if (known) saving.follow()?.heard(known);
    return followServerPageTitle(page.id, (title) => saving.follow()?.heard(title));
  }, [saving, page.id]);
  useEffect(() => {
    if (readOnly) return;
    const change = onTitleChange;
    const follow = saving.follow();
    const current = state.current;
    const save = () => {
      if (current.timer !== null) window.clearTimeout(current.timer);
      current.timer = null;
      const sending = follow?.next();
      // A title of plain words says so: spans it held before go.
      if (sending) follow!.sent(sending.value, change(sending.value.title, sending.base, [...sending.value.spans ?? []]));
    };
    current.save = save;
    // A write changing what the page is sends the title first (lib/pageKindChange.ts).
    const letGo = holdTypedPageTitle(save);
    const onVisibility = () => {
      if (document.visibilityState === "hidden") save();
    };
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("pagehide", save);
    return () => {
      letGo();
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener("pagehide", save);
      save();
    };
    // The handler is this page's: a new title change callback alone doesn't start it over.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [saving, page.id, readOnly]);
  return saving;
}

// The page's title as the server holds it, with its spans while they spell it.
function pageTitleBase(page: Page): PageTitleBase {
  const spelled = page.titleSpans?.length && page.titleSpans.map((span) => span.text).join("") === page.title;
  return { title: page.title, cursor: page.cursor, ...(spelled ? { titleSpans: page.titleSpans } : {}) };
}
