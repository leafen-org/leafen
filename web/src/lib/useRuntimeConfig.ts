import { useEffect, useState } from "react";
import type { PublicRuntimeConfig } from "./runtimeConfigContract";
import { fetchRuntimeConfig, subscribeRuntimeConfig } from "./runtimeConfigService";

// The settings a screen that stays open last showed, so one drawn again in
// this load starts from them rather than from nothing for its first moment.
let shown: PublicRuntimeConfig | null = null;

/**
 * The server's runtime settings for a screen that stays open — the sidebar's
 * workspace menu, say: read once, then again each time they are read anew
 * (refreshRuntimeConfig: an administrator's save, the server answering
 * again), so a setting saved shows in this window at once, not after a
 * reload; null until first read.
 */
export function useRuntimeConfig(): PublicRuntimeConfig | null {
  const [config, setConfig] = useState(shown);
  useEffect(() => {
    let live = true;
    let renewed = false;
    const show = (next: PublicRuntimeConfig) => {
      shown = next;
      if (live) setConfig(next);
    };
    void fetchRuntimeConfig().then(({ config: read }) => { if (!renewed) show(read); });
    const stop = subscribeRuntimeConfig((next) => {
      renewed = true;
      show(next);
    });
    return () => {
      live = false;
      stop();
    };
  }, []);
  return config;
}
