import { useCallback, useEffect, useRef, useState } from "react";
import { isPackagedClient } from "./packagedClient";

/**
 * A page outside Leafen the person goes on to after a press, whose address
 * comes a moment later — Tailscale's sign-in, then its page that allows a
 * public address (2026-10-27: the person was left looking at a waiting line
 * and a button to find). Pressed, Leafen opens each address as it comes, once:
 * the desktop and phone apps hand it to the computer's browser; a page opens
 * an empty tab at the press — a tab opened later is a blocked pop-up — and
 * puts the first address in it, closing it if none comes. A browser cuts the
 * tab off from Leafen as soon as it leaves for another site (Leafen's pages,
 * and Tailscale's own, keep their windows to their own sites), so a page's
 * next address — the page that allows the address, after the sign-in — is
 * the button's to open, as the step says (2026-11-11: the sign-in's tab
 * closed, a new tab was a blocked pop-up and the screen said it had opened).
 * Only a press opens anything: a screen opened on a sign-in already waiting
 * shows its button. "Opened" is said only of a tab that did open, or once
 * the person pressed the button. The page outside isn't told the server's
 * address (sendOn).
 */
export function useOutsideOpener(address: string, finished: boolean) {
  const armed = useRef(false);
  const tab = useRef<Window | null>(null);
  const done = useRef(new Set<string>());
  const [opened, setOpened] = useState("");

  const prepare = useCallback(() => {
    armed.current = true;
    // The app opens addresses in the system's browser; a browser opens its
    // tab now, in the click's turn, or its blocker stops it later.
    if (isPackagedClient() || (tab.current && !tab.current.closed)) return;
    tab.current = window.open("", "_blank");
    if (tab.current) tab.current.opener = null;
  }, []);

  const settle = useCallback(() => {
    if (tab.current && !tab.current.closed && !done.current.size) tab.current.close();
    tab.current = null;
    armed.current = false;
  }, []);

  useEffect(() => {
    if (!address || !armed.current || done.current.has(address)) return;
    done.current.add(address);
    if (isPackagedClient()) {
      setOpened(address);
      void import("@wailsio/runtime").then(({ Browser }) => Browser.OpenURL(address))
        .catch(() => window.open(address, "_blank", "noopener,noreferrer"));
      return;
    }
    // A tab lost or closed leaves the button to press.
    if (tab.current && !tab.current.closed) {
      sendOn(tab.current, address);
      setOpened(address);
    }
  }, [address]);

  useEffect(() => {
    if (finished) settle();
  }, [finished, settle]);
  useEffect(() => settle, [settle]);

  // The person opened it from the button.
  const pressed = useCallback(() => setOpened(address), [address]);

  return { prepare, settle, pressed, opened: Boolean(address) && opened === address };
}

/**
 * Sends the tab on without this page's address as the referrer (2026-11-11:
 * Tailscale's sign-in was sent the server's address). A tab this page sends on
 * goes with this page's referrer policy as it is when the navigation starts —
 * the tab's own page, Tailscale's once signed in, can't be reached to send it
 * — so the page says "no-referrer" for that moment only, then the browsers'
 * default again (Leafen's page sets none of its own).
 */
function sendOn(tab: Window, address: string) {
  const policy = document.createElement("meta");
  policy.name = "referrer";
  policy.content = "no-referrer";
  document.head.append(policy);
  try {
    tab.location.href = address;
  } finally {
    policy.content = "strict-origin-when-cross-origin";
    policy.remove();
  }
}
