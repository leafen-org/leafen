import { useEffect, useRef } from "react";

const RETRY_MS = 30_000;

/**
 * While there is no workspace to show, none left or none read, the list is
 * asked again every so often while the tab is in sight, on coming back
 * online and on coming back to the tab, so a page shared again or a server
 * reached again brings the app back by itself.
 */
export function useNoWorkspaceRetry(waiting: boolean, refresh: () => void) {
  const latest = useRef(refresh);
  latest.current = refresh;
  useEffect(() => {
    if (!waiting) return undefined;
    const again = () => latest.current();
    const visible = () => { if (document.visibilityState === "visible") again(); };
    // Only a tab in sight asks on the clock; one behind asks as it comes back.
    const timer = window.setInterval(visible, RETRY_MS);
    window.addEventListener("online", again);
    document.addEventListener("visibilitychange", visible);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("online", again);
      document.removeEventListener("visibilitychange", visible);
    };
  }, [waiting]);
}
