import { useCallback, useEffect, useRef, useState } from "react";

// An app's page starts in its own default colors and learns Leafen's theme
// when its channel opens, so until then a dark Leafen showed a white frame
// (2026-11-05, on a busy machine for seconds). The frame stays unseen until
// the app has had its theme; a page that opens no channel shows soon after
// it loads.
const AFTER_READY_MS = 80;
const WITHOUT_CHANNEL_MS = 1500;

export function useAppFrameReveal(frame: string) {
  const [shown, setShown] = useState("");
  const timer = useRef(0);
  // The frame that said hello, whose channel decides when it shows.
  const helloed = useRef("");
  useEffect(() => () => window.clearTimeout(timer.current), [frame]);
  const showAfter = useCallback((delay: number) => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setShown(frame), delay);
  }, [frame]);
  return {
    // Unseen by its opacity alone: a frame first laid out hidden or out of
    // reach of presses (visibility, pointer-events) never scrolled by a finger
    // in an iPhone's Safari, until its page was laid out anew (2026-11-14, the
    // drive's screen in Leafen's page; its full window scrolled). Its box
    // takes the presses while it opens (AppFrameBox).
    frameStyle: shown === frame ? undefined : { opacity: 0 },
    /** The frame is still unseen: its box says the app is opening (AppFrameBox). */
    waiting: shown !== frame,
    onChannelReady: useCallback(() => showAfter(AFTER_READY_MS), [showAfter]),
    /** The frame said hello: its channel, not its load, decides when it shows. */
    onChannelOpened: useCallback(() => {
      window.clearTimeout(timer.current);
      helloed.current = frame;
    }, [frame]),
    onFrameLoad: useCallback(() => {
      if (shown !== frame && helloed.current !== frame) showAfter(WITHOUT_CHANNEL_MS);
    }, [frame, shown, showAfter]),
  };
}
