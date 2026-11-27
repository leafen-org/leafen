import { useCallback, useEffect, useRef, useState } from "react";
import type { AppPackageReview } from "@/appPlatformWire.generated";

/**
 * A package being read before its review: an app file the person chose, or
 * an app a catalog holds. Its window says which and what it is doing from
 * the press on, and why it stopped if it did (2026-11-12: a 78 MB file left
 * the settings dimmed for 10–15 seconds with no word).
 */
export type AppPackageReading = {
  source: "file" | "catalog";
  /** The file's name, or the app's; none when only its address is known. */
  name?: string;
  /** The file's size, or the catalog app's version. */
  detail?: string;
  /** Why it couldn't be read; its window offers another try. */
  failure?: string;
};

type Read = {
  what: Omit<AppPackageReading, "failure">;
  load: (signal: AbortSignal) => Promise<AppPackageReview>;
  failureText: (failure: unknown) => string;
};

/**
 * Reads one package at a time for its review: a new read, or closing the
 * window, stops the one before. The review is handed on once read.
 */
export function useAppPackageRead(onReview: (review: AppPackageReview) => void) {
  const [reading, setReading] = useState<AppPackageReading | null>(null);
  const latest = useRef(onReview);
  latest.current = onReview;
  const current = useRef<{ read: Read; abort: AbortController } | null>(null);
  useEffect(() => () => current.current?.abort.abort(), []);

  const start = useCallback((read: Read) => {
    current.current?.abort.abort();
    const abort = new AbortController();
    current.current = { read, abort };
    setReading(read.what);
    read.load(abort.signal).then((review) => {
      if (abort.signal.aborted) return;
      current.current = null;
      setReading(null);
      latest.current(review);
    }, (failure: unknown) => {
      if (abort.signal.aborted) return;
      setReading({ ...read.what, failure: read.failureText(failure) });
    });
  }, []);

  const read = useCallback((what: Read["what"], load: Read["load"], failureText: Read["failureText"]) => {
    start({ what, load, failureText });
  }, [start]);

  /** The same read once more, after it failed. */
  const again = useCallback(() => {
    if (current.current) start(current.current.read);
  }, [start]);

  const stop = useCallback(() => {
    current.current?.abort.abort();
    current.current = null;
    setReading(null);
  }, []);

  return { reading, read, again, stop };
}
