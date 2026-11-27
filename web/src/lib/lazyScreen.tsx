import { lazy, useEffect, useState, type ComponentProps, type ComponentType, type ReactNode } from "react";
import { ScreenLoadBoundary } from "@/components/ScreenLoadBoundary";
import { screenLoadFailure, withLoadDeadline } from "./screenLoad";
import { fetchAhead, fetchAheadInTurn, holdStartFor } from "./startSettled";

export { screenCodeStillComing, withLoadDeadline } from "./screenLoad";

/**
 * React's lazy, its code bounded by the wait, and fetched ahead: once the
 * page's start has settled, a couple at a time (startSettled.ts
 * fetchAheadInTurn), so that the press that opens a window, a panel or a view
 * draws it on its own frame. React holds back a part whose code comes after
 * it is asked for, and shows it no sooner than 300 ms after its stand-in:
 * fetched at the press, the inbox, "멤버 초대" and the other windows a press
 * opens first showed 360–480 ms after it on a NAS with a device half as fast
 * (2026-11-14). A screen an address opens as the page loads passes `entry`
 * (the app's own routes, a consent page, a join link): drawn with the start
 * that needs it and opened by no press, it isn't fetched ahead.
 * A window that takes typing as it opens (a search or a name to fill in)
 * passes `typedOnOpen`: its code comes sooner (startSettled.ts: from the
 * offline worker's copy at once, over the network once the first screen is
 * drawn), so keys typed right after opening it reach it.
 * Fetched on the first opening, those keys went to what still had the focus —
 * a name typed right after "새 워크스페이스" was lost, and the space in a search
 * typed right after a sidebar row's "옮기기" opened that row's page (2026-11-05).
 * A part a press draws on its own frame and wanted before the rest passes
 * `drawnOnPress`: from the offline worker's copy at once, over the network
 * once the start has settled, not in turn (2026-11-13: a "/" database's
 * table 330 ms after its Enter). A window kept on the page shut (`open` false) fetches its code
 * ahead too, not as it is put there: the import window's, put there as a
 * workspace showed, took the connections the workspace's first reads went out
 * on (2026-11-13). `preload` fetches the code now, for a place that knows it
 * is about to draw the part; code that is here is handed over in the same
 * pass.
 *
 * Code that doesn't come stays the trouble of the place it draws: the window,
 * panel or view says so in place, and that place alone is drawn again once the
 * code can come (ScreenLoadBoundary). Before, the boundary around the whole app
 * took it, and the settings window's code missing offline put its card over the
 * sidebar and the page too (2026-11-11). `standIn` is what a small part shows
 * instead of the card: a highlight its plain code, a helper nothing.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function lazyScreen<T extends ComponentType<any>>(factory: () => Promise<{ default: T }>, options?: {
  typedOnOpen?: boolean;
  drawnOnPress?: boolean;
  entry?: boolean;
  standIn?: (props: ComponentProps<T>) => ReactNode;
}) {
  let loaded: { default: T } | null = null;
  let markCame = () => {};
  const came = new Promise<void>((resolve) => { markCame = resolve; });
  const load = () => factory().then((module) => { loaded = module; markCame(); return module; }, (error: unknown) => {
    throw screenLoadFailure(error);
  });
  const opened = () => holdStartFor(watchScreenLoad(load()));
  let warming = false;
  const warm = () => {
    if (warming) return;
    warming = true;
    fetchAhead(() => { void load().catch(() => undefined); }, options?.typedOnOpen);
  };
  if (options?.typedOnOpen || options?.drawnOnPress) warm();
  else if (!options?.entry) fetchAheadInTurn(() => (loaded || warming ? Promise.resolve() : load()));
  // React's lazy keeps a failure for good: a load tried again is a lazy of its own.
  const makeLazy = () => {
    const made = {
      failed: false,
      // Code already here is handed over at once, so the window shows in the same
      // pass as its opening: React's lazy waits a turn on a promise even when it
      // has settled, and the keys of that turn went to the page beneath (90ms
      // under load).
      // (React's lazy takes any thenable; only its type asks for a promise.)
      view: lazy(() => (loaded
        ? { then: (take: (module: { default: T }) => void) => take(loaded!) } as unknown as Promise<{ default: T }>
        : withLoadDeadline(opened()).catch((error: unknown) => {
          made.failed = true;
          throw error;
        }))),
    };
    return made;
  };
  let current = makeLazy();
  function Screen(props: ComponentProps<T>) {
    const [tries, setTries] = useState({ count: 0, serverHadIt: false });
    const View = current.view as unknown as ComponentType<ComponentProps<T>>;
    const shown = props as { open?: unknown; onClose?: unknown };
    const onClose = typeof shown.onClose === "function" ? shown.onClose as () => void : undefined;
    // Shut, it draws nothing until opened or its code has come, which it
    // doesn't ask for meanwhile; then it is put there shut, as before.
    const shut = shown.open === false && !loaded;
    const [, setCame] = useState(false);
    useEffect(() => {
      if (!shut) return undefined;
      warm();
      let live = true;
      void came.then(() => { if (live) setCame(true); });
      return () => { live = false; };
    }, [shut]);
    if (shut) return null;
    return (
      <ScreenLoadBoundary
        key={tries.count}
        serverHadIt={tries.serverHadIt}
        // A window shut meanwhile (search, import) says nothing until it opens.
        standIn={shown.open === false ? null : options?.standIn?.(props)}
        onClose={onClose}
        retry={(serverHadIt) => {
          if (current.failed) current = makeLazy();
          setTries((last) => ({ count: last.count + 1, serverHadIt }));
        }}
      >
        <View {...props} />
      </ScreenLoadBoundary>
    );
  }
  return Object.assign(Screen, { preload: load });
}

// A screen opened whose code is slow to come says so while it comes: the
// window it opens shows nothing until then, and a press that seemed to do
// nothing for half a minute was pressed again or given up on (2026-11-07, a
// stalled connection on a new build). The deadline above still ends the wait.
const SLOW_SCREEN_LOAD_MS = 800;
let slowScreenLoads = 0;
const slowScreenListeners = new Set<() => void>();

function watchScreenLoad<T>(load: Promise<T>): Promise<T> {
  let slow = false;
  const timer = setTimeout(() => {
    slow = true;
    slowScreenLoads += 1;
    for (const listener of slowScreenListeners) listener();
  }, SLOW_SCREEN_LOAD_MS);
  const done = () => {
    clearTimeout(timer);
    if (!slow) return;
    slowScreenLoads -= 1;
    for (const listener of slowScreenListeners) listener();
  };
  load.then(done, done);
  return load;
}

/** Whether a screen opened is still waiting for its code past a moment. */
export function screenLoadSlow() {
  return slowScreenLoads > 0;
}

export function subscribeSlowScreenLoads(listener: () => void) {
  slowScreenListeners.add(listener);
  return () => { slowScreenListeners.delete(listener); };
}
