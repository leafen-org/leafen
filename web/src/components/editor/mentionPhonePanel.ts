import { placeAround, type PopoverRequest, type PopoverSize, type PopoverViewport } from "@/lib/popoverPlacement";
import { largeViewportHeight } from "@/lib/softKeyboard";

/**
 * On a phone the "@", "[[" and "+" list of the page's own words (its lines
 * and title) is a panel across the screen, 264 high, its foot on the
 * keyboard's top — the foot of what shows of the page — whatever line it was
 * typed on, over that line too where it is low, as Notion's Android app
 * (seen 2026-11-21 on a Galaxy A90, w2270, twice: 264dp from the keyboard's
 * top up, rows 46dp; the line typed at the page's foot under it). Its tools
 * give way while it is open (NativePhoneToolbar.module.css). A tap outside it
 * closes it and goes no further — on the words typed in too, the caret
 * staying after its "@" and the keyboard as it was (MentionMenu,
 * stopTapAtLayer) —, where the phone web's went on to the words tapped.
 * Notion's iPhone app is not seen yet (no test iPhone); a comment's, a
 * cell's and an automation's lists keep the phone web's floating list
 * (phoneListPlace), the app's not seen. Here: where it stands, over the
 * foot of what shows, from the screen's one side to the other (its height
 * is its CSS's, MentionPhonePanel.module.css).
 */
export function placePhoneListPanel(size: PopoverSize, viewport: PopoverViewport): PopoverRequest {
  // iPhone Safari draws the page on under its keyboard, its visual viewport
  // panned over the window; Android Chrome's window ends at the keyboard.
  const shown = window.visualViewport;
  const foot = shown ? shown.offsetTop + shown.height : window.innerHeight;
  return placeAround({ top: foot, bottom: foot, left: 0, right: window.innerWidth }, size, viewport,
    { prefer: "above", gap: 0, margin: 0, marginY: 0, keepSide: true });
}

/** A phone web's list: 30% of the large viewport high, choosing its side by that height (MentionMenu). */
export function phoneListPlace() {
  const cap = largeViewportHeight() * 0.3;
  return { gap: 12, cap, lineSide: cap };
}
