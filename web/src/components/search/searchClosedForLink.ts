import { useClosedForLink } from "@/lib/productWindows";
import { searchIsOpen } from "@/lib/searchOpen";

/**
 * Search (⌘K) closes its own way, its query and filters cleared as its close
 * clears them, when a link goes to a place in the app while it is open
 * (closeWindowsForLink). A result search opens goes through the router too
 * and closes search itself right after: the link's close waits for the end
 * of that task and does nothing once search is closed.
 */
export function useSearchClosedForLink<Close extends () => void>(close: Close): Close {
  useClosedForLink(() => queueMicrotask(() => {
    if (searchIsOpen()) close();
  }));
  return close;
}
