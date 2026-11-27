import { type SyntheticEvent, useRef } from "react";
import { appleTouchDevice } from "@/lib/iosViewport";

/**
 * How an iPhone's or iPad's Safari is brought to draw a video's own bar and
 * first frame before it plays, as Notion's there: Notion asks the video to
 * play as it loads and stops it as it starts loading (read 2026-11-15 in iPhone
 * Safari: autoplay, paused at "loadstart"). Without the ask Safari draws its
 * big play button alone.
 *
 * Safari may still start a video without sound a moment later (seen
 * 2026-11-15: in two of five loads, 60–110 ms after "loadstart"), and a pause
 * inside that "play" does not hold, so until the video is pressed or keyed such
 * a start is undone just after it by loading the video again, which stops it
 * at its still. Elsewhere this does nothing.
 */
export function useAppleVideoHold() {
  const held = useRef(appleTouchDevice());
  return {
    autoPlay: held.current,
    onLoadStart: (event: SyntheticEvent<HTMLVideoElement>) => {
      if (held.current) event.currentTarget.pause();
    },
    /** Whether this start was Safari's own, now stopped: the player then reports no start. */
    stopped(event: SyntheticEvent<HTMLVideoElement>) {
      if (!held.current) return false;
      const video = event.currentTarget;
      video.pause();
      // Loaded again, it stands paused at its still with its bar, as at first.
      setTimeout(() => {
        if (held.current) video.load();
      }, 0);
      return true;
    },
    /** A press or key on the video lets it play from then on. */
    release() {
      held.current = false;
    },
  };
}
