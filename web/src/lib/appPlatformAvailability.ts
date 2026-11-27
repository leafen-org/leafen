import { useEffect, useState } from "react";
import { fetchRuntimeConfig } from "@/lib/runtimeConfigService";
import { serverOrigin } from "@/lib/serverOrigin";

// Whether this server's build has the app platform (a certified build leaves
// it out). The answer is read once a server and kept, so a menu that opens
// later starts from it rather than from "no" for its first moment; a
// packaged app reads it from the server it picks, not before (2026-10-26).

type Answer = { server: string; known: boolean | null; missing: "" | "phone" | "store"; reading: Promise<boolean> };
let answer: Answer | null = null;

function current(): Answer | null {
  if (typeof window === "undefined") return null;
  const server = serverOrigin();
  if (answer?.server !== server) {
    // Why a build has none: a phone app's own server, or the App Store edition.
    const next: Answer = { server, known: null, missing: "", reading: Promise.resolve(false) };
    next.reading = fetchRuntimeConfig().then(({ config }) => {
      next.known = config.appPlatform;
      next.missing = config.appPlatformMissing;
      return next.known;
    }, () => false);
    answer = next;
  }
  return answer;
}

export function useAppPlatformAvailable() {
  return useAppPlatformKnown() ?? false;
}

/** Whether the app platform is there, or null until the server has said. */
export function useAppPlatformKnown(): boolean | null {
  const [available, setAvailable] = useState(() => current()?.known ?? null);
  useEffect(() => {
    let live = true;
    void current()?.reading.then((value) => { if (live) setAvailable(value); });
    return () => { live = false; };
  }, []);
  return available;
}

/** Whether the app platform is there, read once for every caller. */
export function readAppPlatformAvailable(): Promise<boolean> {
  return current()?.reading ?? Promise.resolve(false);
}

/** Why this server has no app platform, which the apps' places say ("" when it has it or no reason is said). */
export function useAppPlatformMissing() {
  const [reason, setReason] = useState(() => current()?.missing ?? "");
  useEffect(() => {
    let live = true;
    const read = current();
    void read?.reading.then(() => { if (live) setReason(read.missing); });
    return () => { live = false; };
  }, []);
  return reason;
}
