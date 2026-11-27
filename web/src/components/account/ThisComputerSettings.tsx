import { useEffect, useState } from "react";
import { desktopBackground, desktopBackgroundAvailable, type DesktopBackgroundState } from "@/lib/desktopBackground";
import { desktopNetworkProxy, type DesktopNetworkProxyState } from "@/lib/desktopNetworkProxy";
import { DesktopBackground } from "./DesktopBackground";
import { DesktopNetworkProxy } from "./DesktopNetworkProxy";

/**
 * The desktop app's own settings under 서버 계정 — 이 컴퓨터 and its network
 * proxy — read together and drawn together, below what the page already
 * shows: each drawn as its own read came moved the one below it down
 * (2026-11-06). A part whose read failed is left out, as before.
 */
/** `held`: read now, drawn only once released (what comes above it may still come). */
export function ThisComputerSettings({ held = false }: { held?: boolean } = {}) {
  const shown = desktopBackgroundAvailable();
  const [read, setRead] = useState<{ background: DesktopBackgroundState | null; proxy: DesktopNetworkProxyState | null } | null>(null);
  useEffect(() => {
    if (!shown) return undefined;
    let live = true;
    void Promise.allSettled([desktopBackground.state(), desktopNetworkProxy.state()]).then(([background, proxy]) => {
      if (live) setRead({
        background: background.status === "fulfilled" ? background.value : null,
        proxy: proxy.status === "fulfilled" ? proxy.value : null,
      });
    });
    return () => { live = false; };
  }, [shown]);
  if (!shown || !read || held) return null;
  return (
    <>
      <DesktopBackground initial={read.background} />
      <DesktopNetworkProxy initial={read.proxy} />
    </>
  );
}
