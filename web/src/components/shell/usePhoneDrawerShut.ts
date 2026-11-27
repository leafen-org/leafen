import { useLayoutEffect, type RefObject } from "react";
import { holdFocusIn } from "@/lib/modalFocus";

/**
 * While a phone's sidebar drawer is open, what lies beside it is shut: the
 * page and a band above it, hidden from assistive tools (AppShell), a row's
 * page opened whole over the page (DatabaseRowPage, drawn in the layers' box
 * before the drawer opened) hidden here, all of it kept from the focus and
 * Tab going round the drawer's own controls (holdFocusIn) — not made inert:
 * turning the page inert worked out the style of all of it again as the
 * drawer opened and as it closed (2026-11-13 in iPhone Safari, a page of 38k
 * elements: 289–305 ms each way; lib/modalFocus.ts). The root is marked too,
 * for that row's page to slide aside with the page
 * (DatabaseRowPeekBody.module.css), which the shell's own mark doesn't reach.
 */
export function usePhoneDrawerShut(open: boolean, drawer: RefObject<HTMLElement | null>) {
  useLayoutEffect(() => {
    if (!open || !drawer.current) return undefined;
    const root = document.documentElement;
    root.dataset.phoneDrawer = "open";
    const release = holdFocusIn(drawer.current);
    return () => {
      release();
      delete root.dataset.phoneDrawer;
    };
  }, [open, drawer]);
}
