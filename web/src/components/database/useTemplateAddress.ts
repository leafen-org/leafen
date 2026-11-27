import { useContext, useEffect, useRef, useState } from "react";
import type { DatabaseToolbarTemplateMetadataWire } from "@/databaseToolbarConfigWire.generated";
import { pageHref } from "@/lib/navigation";
import { pushRoute, replaceRoute, ROUTE_CHANGE_EVENT } from "@/lib/router";
import { OpenDatabasePagesContext } from "./databaseRowOpeners";
import { useRowWindowTemplateAddress } from "./rowWindowTemplate";

/**
 * Where a database's template shows is in the address, as Notion's (seen
 * 2026-11-25 in Chrome at 1440, two tabs: a synthetic database's template):
 * its window over its database's page names it there — in the middle
 * "?p=〈template〉", beside it "&pm=s", the view menu's switch a step of none,
 * a reload keeping either — and opened whole (⤢, from either) it goes to its
 * own address /p/〈template〉 as a step of its own, back bringing its window
 * again and a reload or that address followed opening it whole; its link,
 * from its window or whole, is its own address. Leafen names it in its
 * window with its own parameter, kept apart from an opened row's, and opens
 * an old whole one (`&pm=f` over its database's page) at its own address.
 */
export const TEMPLATE_PAGE_PARAM = "tp";

/** The address a template's link copies, from its window or whole: its own page's. */
export function templateAddress(templateId: string) {
  return pageHref(templateId);
}

/** How a template's window over its database's page shows (DatabaseTemplateEditor). */
export type TemplateWindowMode = "side" | "center" | "full";

// The template's window's own, beside or in the middle: the address names it with no row open (`p`).
function addressedMode(params: URLSearchParams, templateId: string) {
  return params.get(TEMPLATE_PAGE_PARAM) === templateId && !params.has("p") ? params.get("pm") : null;
}

/** How a template's window opens: as the address names it, beside or in the middle. */
export function templateWindowMode(templateId: string): TemplateWindowMode {
  return addressedMode(new URL(window.location.href).searchParams, templateId) === "s" ? "side" : "center";
}

/** Its window turned beside or to the middle names that in the address, as no step of its own. */
export function showTemplateWindowMode(templateId: string, mode: Exclude<TemplateWindowMode, "full">) {
  const url = new URL(window.location.href);
  if (url.searchParams.get(TEMPLATE_PAGE_PARAM) !== templateId || url.searchParams.has("p")) return;
  if (mode === "side") url.searchParams.set("pm", "s");
  else url.searchParams.delete("pm");
  window.history.replaceState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
}

// The template opened whole a moment ago, which its own address draws at once from (StandaloneRowTemplates).
let handedWhole: { template: DatabaseToolbarTemplateMetadataWire; at: number } | null = null;
const HANDED_WHOLE_FOR_MS = 3000;

/** Opens a template whole at its own address, its page drawing from what its window held. */
export function openTemplateWhole(template: DatabaseToolbarTemplateMetadataWire) {
  handedWhole = { template, at: Date.now() };
  pushRoute(templateAddress(template.id));
}

/** The template a window just opened whole, for its own address to draw at once. */
export function templateOpenedWhole(templateId: string) {
  return handedWhole?.template.id === templateId && Date.now() - handedWhole.at < HANDED_WHOLE_FOR_MS ? handedWhole.template : null;
}

// A template named over a row kept beneath it (`pp` without `p`) is the row window's view's (rowWindowTemplate.ts).
// One named whole there (`pm=f`), an older address's, opens at its own address instead.
function namedTemplate() {
  const url = new URL(window.location.href), params = url.searchParams, named = params.get(TEMPLATE_PAGE_PARAM);
  if (named && addressedMode(params, named) === "f") {
    replaceRoute(`${templateAddress(named)}${url.hash}`);
    return null;
  }
  return params.has("pp") && !params.has("p") ? null : named;
}

function publish(url: URL) {
  window.history.pushState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
}

// The toolbar whose window shows the template the address names, by template:
// the first on the screen to hold it, so two views of one database open it once.
const addressWindows = new Map<string, symbol>();

/**
 * Opens the template the address names, one of these, and keeps the address with the one open.
 * One toolbar opens it, the first holding it; a toolbar inside a database page's window — a
 * template's or a row's (OpenDatabasePagesContext) — leaves the address to the window's own and
 * opens its templates by its presses alone, so a view of the same database in a template's body
 * doesn't open that template over itself again and again (2026-11-16: 12 windows on a reload); one
 * in a row's window the address names opens it in the window's place (rowWindowTemplate.ts).
 */
export function useTemplateAddress(templateIds: readonly string[], openId: string | null, open: (templateId: string | null) => void) {
  const openPages = useContext(OpenDatabasePagesContext), inWindow = openPages.length > 0;
  const [self] = useState(() => Symbol("template-window"));
  const ids = useRef(templateIds);
  ids.current = templateIds;
  const latestOpen = useRef(open);
  latestOpen.current = open;
  // Whether this toolbar may show the one named: no other toolbar's window shows it.
  const claim = useRef((templateId: string) => {
    const holder = addressWindows.get(templateId);
    if (holder && holder !== self) return false;
    addressWindows.set(templateId, self);
    return true;
  }).current;
  useRowWindowTemplateAddress({ param: TEMPLATE_PAGE_PARAM, rowId: inWindow ? openPages.at(-1) : undefined,
    templateIds: ids, openId, open: latestOpen, claim });
  // A link names one, which opens once its database's templates are read.
  const key = templateIds.join();
  useEffect(() => {
    if (inWindow) return;
    const named = namedTemplate();
    if (named && ids.current.includes(named) && claim(named)) latestOpen.current(named);
  }, [claim, inWindow, key]);
  // Back and forward, and a link followed here (a favorite's in the
  // sidebar), open the one the address names, or close it.
  useEffect(() => {
    if (inWindow) return;
    const follow = () => {
      const named = namedTemplate();
      latestOpen.current(named && ids.current.includes(named) && claim(named) ? named : null);
    };
    window.addEventListener("popstate", follow);
    window.addEventListener(ROUTE_CHANGE_EVENT, follow);
    return () => { window.removeEventListener("popstate", follow); window.removeEventListener(ROUTE_CHANGE_EVENT, follow); };
  }, [claim, inWindow]);
  // Opening one adds it to the address; closing it takes it out, the one this toolbar showed.
  // Each is a step of its own, as Notion's: its Escape and a press beside it push the page
  // without the template, so back opens the template again (seen 2026-11-16, two tabs).
  const shown = useRef<string | null>(null);
  useEffect(() => {
    if (inWindow) return;
    const was = shown.current;
    shown.current = openId;
    if (was && was !== openId && addressWindows.get(was) === self) addressWindows.delete(was);
    const url = new URL(window.location.href);
    const named = url.searchParams.get(TEMPLATE_PAGE_PARAM);
    if (openId) addressWindows.set(openId, self);
    // How its window shows (`pm`, templateWindowMode) goes with it: one opened afresh opens in the middle.
    const ownMode = !url.searchParams.has("p");
    if (openId && named !== openId) {
      url.searchParams.set(TEMPLATE_PAGE_PARAM, openId);
      if (ownMode && !was) url.searchParams.delete("pm");
      publish(url);
    } else if (!openId && was && named === was) {
      url.searchParams.delete(TEMPLATE_PAGE_PARAM);
      if (ownMode) url.searchParams.delete("pm");
      publish(url);
    }
  }, [inWindow, openId, self]);
  useEffect(() => () => {
    for (const [templateId, holder] of addressWindows) if (holder === self) addressWindows.delete(templateId);
  }, [self]);
}
