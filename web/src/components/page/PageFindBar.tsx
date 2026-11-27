import { useEditTimeKnownHere } from "@/lib/pageEditedHere";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";
import {
  type KeyboardEvent as ReactKeyboardEvent,
  type RefObject,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import { motionSafeScrollBehavior } from "@/lib/motion";
import { pageFindTextRanges } from "@/lib/pageFindText";
import { PAGE_FIND_SOURCE_ATTRIBUTE, pageFindSourceOf } from "@/lib/pageFindSources";
import { ArrowDown, ArrowUp, Search, X } from "@/icons/leafen";
import styles from "./PageView.module.css";
import { chordLetter, primaryModifier } from "@/lib/keyboardShortcuts";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

const FIND_MATCH_HIGHLIGHT = "leafen-page-find-match";
const FIND_ACTIVE_HIGHLIGHT = "leafen-page-find-active";
const FIND_STYLE_ID = "leafen-page-find-highlight-style";

type HighlightRegistry = {
  set: (name: string, highlight: unknown) => void;
  delete: (name: string) => void;
};

type HighlightApi = {
  Highlight: new (...ranges: Range[]) => unknown;
  registry: HighlightRegistry;
};

// A match drawn on the page, or one a part of the page found in its data
// (drawn only once revealed).
type FindMatch = {
  range(): Range | null;
  reveal?(): void;
};

export function selectedTextForPageFind(root: HTMLElement | null) {
  if (!root || typeof window === "undefined") return "";
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed || selection.rangeCount === 0) return "";

  const ranges = Array.from({ length: selection.rangeCount }, (_, index) => selection.getRangeAt(index));
  if (!ranges.some((range) => range.intersectsNode(root))) return "";

  return selection
    .toString()
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, 140);
}

function pageFindApi(): HighlightApi | null {
  if (typeof window === "undefined" || typeof CSS === "undefined") return null;
  const registry = (CSS as unknown as { highlights?: HighlightRegistry }).highlights;
  const Highlight = (window as unknown as { Highlight?: new (...ranges: Range[]) => unknown }).Highlight;
  return registry && Highlight ? { registry, Highlight } : null;
}

function clearPageFindHighlights() {
  const api = pageFindApi();
  api?.registry.delete(FIND_MATCH_HIGHLIGHT);
  api?.registry.delete(FIND_ACTIVE_HIGHLIGHT);
}

function ensurePageFindHighlightStyle() {
  if (typeof document === "undefined") return;
  if (document.getElementById(FIND_STYLE_ID)) return;
  const style = document.createElement("style");
  style.id = FIND_STYLE_ID;
  style.textContent = `
::highlight(${FIND_MATCH_HIGHLIGHT}) {
  background: var(--page-find-match-bg, rgba(255, 212, 0, 0.38));
  color: inherit;
}
::highlight(${FIND_ACTIVE_HIGHLIGHT}) {
  background: var(--page-find-active-bg, rgba(255, 176, 0, 0.72));
  color: inherit;
}`;
  document.head.appendChild(style);
}

function searchableTextNodes(root: HTMLElement) {
  const nodes: Text[] = [];
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      const value = node.nodeValue ?? "";
      if (!value.trim()) return NodeFilter.FILTER_REJECT;
      const parent = node.parentElement;
      if (!parent) return NodeFilter.FILTER_REJECT;
      if (
        parent.closest(
          `script, style, noscript, button, input, select, textarea, [aria-hidden="true"], [data-page-find-exclude], [${PAGE_FIND_SOURCE_ATTRIBUTE}]`,
        )
      ) {
        return NodeFilter.FILTER_REJECT;
      }
      const editableAncestor = parent.closest('[contenteditable="true"]');
      if (!editableAncestor && parent.closest('[contenteditable="false"]')) {
        return NodeFilter.FILTER_REJECT;
      }
      return NodeFilter.FILTER_ACCEPT;
    },
  });

  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    nodes.push(node as Text);
  }
  return nodes;
}

// The page's matches in order: its drawn text, and in their places the
// matches of the parts found from their data.
function pageFindMatches(root: HTMLElement, query: string): FindMatch[] {
  if (!query) return [];
  const matches: FindMatch[] = [];
  const sources = Array.from(root.querySelectorAll(`[${PAGE_FIND_SOURCE_ATTRIBUTE}]`));
  let nextSource = 0;
  const takeSourcesBefore = (node: Node | null) => {
    while (nextSource < sources.length && (!node
      || sources[nextSource].compareDocumentPosition(node) & Node.DOCUMENT_POSITION_FOLLOWING)) {
      for (const match of pageFindSourceOf(sources[nextSource])?.find(query) ?? []) matches.push(match);
      nextSource += 1;
    }
  };

  for (const node of searchableTextNodes(root)) {
    takeSourcesBefore(node);
    const text = node.nodeValue ?? "";
    for (const match of pageFindTextRanges(text, query)) {
      const range = document.createRange();
      range.setStart(node, match.start);
      range.setEnd(node, match.end);
      matches.push({ range: () => range });
    }
  }
  takeSourcesBefore(null);

  return matches;
}

function applyPageFindHighlights(matches: FindMatch[], activeIndex: number) {
  const api = pageFindApi();
  if (!api) return;
  api.registry.delete(FIND_MATCH_HIGHLIGHT);
  api.registry.delete(FIND_ACTIVE_HIGHLIGHT);
  const drawn = matches.map((match) => match.range()).filter((range): range is Range => range !== null);
  if (drawn.length === 0) return;
  api.registry.set(FIND_MATCH_HIGHLIGHT, new api.Highlight(...drawn));
  const active = matches[activeIndex]?.range();
  if (active) {
    api.registry.set(FIND_ACTIVE_HIGHLIGHT, new api.Highlight(active));
  }
}

export function PageFindBar({
  focusTick,
  initialQuery = "",
  onClose,
  open,
  pageId,
  revision,
  rootRef,
}: {
  focusTick: number;
  initialQuery?: string;
  onClose: () => void;
  open: boolean;
  pageId: string;
  revision: string;
  rootRef: RefObject<HTMLElement | null>;
}) {
  const { t } = useTranslation("pageFindBar");
  const inputRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState("");
  const [matches, setMatches] = useState<FindMatch[]>([]);
  const [activeIndex, setActiveIndex] = useState(-1);
  const normalizedQuery = query.trim();
  // An edit made here, which the page's own revision doesn't show, finds again too (lib/pageEditedHere.ts).
  const editedHere = useEditTimeKnownHere(open ? pageId : "");

  const step = useCallback((delta: number) => {
    setActiveIndex((current) => {
      if (matches.length === 0) return -1;
      const base = current < 0 ? 0 : current;
      return (base + delta + matches.length) % matches.length;
    });
  }, [matches.length]);

  useEffect(() => {
    if (!open) return;
    ensurePageFindHighlightStyle();
    const seededQuery = initialQuery.trim();
    if (seededQuery) setQuery(seededQuery);
    // Its words chosen once drawn, what is typed from the press going in (focusOpenedField).
    return focusOpenedField(() => inputRef.current, { select: true, scroll: false, from: 2 });
  }, [focusTick, initialQuery, open]);

  useEffect(() => {
    if (!open || !normalizedQuery) {
      clearPageFindHighlights();
      const frame = window.requestAnimationFrame(() => {
        setMatches([]);
        setActiveIndex(-1);
      });
      return () => window.cancelAnimationFrame(frame);
    }
    const frame = window.requestAnimationFrame(() => {
      const root = rootRef.current;
      const nextMatches = root ? pageFindMatches(root, normalizedQuery) : [];
      setMatches(nextMatches);
      setActiveIndex(nextMatches.length ? 0 : -1);
    });
    return () => window.cancelAnimationFrame(frame);
  }, [editedHere, normalizedQuery, open, pageId, revision, rootRef]);

  useEffect(() => {
    if (!open) {
      clearPageFindHighlights();
      return;
    }
    applyPageFindHighlights(matches, activeIndex);
    // A part found from its data marks its matches as it draws them.
    let frame = 0;
    const redraw = new MutationObserver(() => {
      window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(() => applyPageFindHighlights(matches, activeIndex));
    });
    for (const source of rootRef.current?.querySelectorAll(`[${PAGE_FIND_SOURCE_ATTRIBUTE}]`) ?? []) {
      redraw.observe(source, { childList: true, subtree: true });
    }
    return () => {
      redraw.disconnect();
      window.cancelAnimationFrame(frame);
      clearPageFindHighlights();
    };
  }, [activeIndex, matches, open, rootRef]);

  useEffect(() => {
    if (!open || activeIndex < 0) return;
    const match = matches[activeIndex];
    if (!match) return;
    // A match found from data is drawn first, then brought to the middle.
    match.reveal?.();
    let frames = 0;
    let frame = 0;
    const show = () => {
      const range = match.range();
      if (!range) {
        if (++frames < 20) frame = window.requestAnimationFrame(show);
        return;
      }
      applyPageFindHighlights(matches, activeIndex);
      const rect = range.getBoundingClientRect();
      const target = rect.width > 0 || rect.height > 0
        ? document.elementFromPoint(rect.left, rect.top)
        : null;
      // A match found from data is centred by its own element: its block may
      // be a long list that would carry it out of the drawn rows.
      const element = match.reveal ? range.startContainer.parentElement :
        target?.closest<HTMLElement>("[data-block-id]") ??
        target?.closest<HTMLElement>("[data-page-search-root]") ??
        target?.closest<HTMLElement>("[data-row-peek-search-root]") ??
        range.commonAncestorContainer.parentElement;
      element?.scrollIntoView({
        block: "center",
        inline: "nearest",
        behavior: motionSafeScrollBehavior(),
      });
    };
    frame = window.requestAnimationFrame(show);
    return () => window.cancelAnimationFrame(frame);
  }, [activeIndex, matches, open]);

  useEffect(() => clearPageFindHighlights, []);

  if (!open) return null;

  function onFindBarKeyDown(e: ReactKeyboardEvent<HTMLElement>) {
    if (e.defaultPrevented) return;
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
      return;
    }

    const key = chordLetter(e);
    const mod = primaryModifier(e);
    const repeatShortcut =
      (mod && !e.altKey && key === "g") ||
      (!mod && !e.altKey && e.key === "F3");
    if (!repeatShortcut) return;

    e.preventDefault();
    step(e.shiftKey ? -1 : 1);
  }

  const countLabel = normalizedQuery ? `${activeIndex >= 0 ? activeIndex + 1 : 0}/${matches.length}` : "0/0";

  return (
    <div
      className={styles.findBar}
      role="dialog"
      aria-label={t("dialog.label")}
      data-page-find-exclude
      onKeyDown={onFindBarKeyDown}
    >
      <div className={styles.findInputWrap}>
        <Search size="small" aria-hidden="true" />
        <input
          ref={inputRef}
          value={query}
          aria-label={t("dialog.label")}
          aria-keyshortcuts="Enter Shift+Enter Meta+G Control+G F3"
          placeholder={t("input.placeholder")}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (imeComposing(e)) return;
            if (fieldEnter(e)) {
              e.preventDefault();
              step(e.shiftKey ? -1 : 1);
            }
          }}
        />
      </div>
      <span className={styles.findCount} aria-live="polite">
        {countLabel}
      </span>
      <button
        type="button"
        className={styles.findButton}
        onClick={() => step(-1)}
        disabled={matches.length === 0}
        aria-label={t("actions.previous")}
      >
        <ArrowUp size="small" />
      </button>
      <button
        type="button"
        className={styles.findButton}
        onClick={() => step(1)}
        disabled={matches.length === 0}
        aria-label={t("actions.next")}
      >
        <ArrowDown size="small" />
      </button>
      <button type="button" className={styles.findButton} onClick={onClose} aria-label={t("actions.close")}>
        <X size="small" />
      </button>
    </div>
  );
}
