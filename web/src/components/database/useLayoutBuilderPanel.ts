import { useEffect, useState } from "react";
import "./layoutBuilderNarrow.css";

/** The window under which the builder folds its panel's column (layoutBuilderNarrow.css). */
export const NARROW_LAYOUT_BUILDER = "(max-width: 900px)";

const narrowWindow = () => typeof window !== "undefined" && window.matchMedia(NARROW_LAYOUT_BUILDER).matches;

/**
 * Whether the builder shows its panel's column, as Notion's (seen 2026-11-24
 * in two Chrome tabs): unfolded on a window over 900px, folded on one of
 * 900 or less, where its "«" lays the column over the page; the window
 * crossing 900 either way sets it back so (folded at 1100, narrowed to 900
 * and widened again, it stood unfolded), whatever "«" or "»" chose before.
 */
export function useLayoutBuilderPanel() {
  const [shown, setShown] = useState(() => !narrowWindow());
  useEffect(() => {
    const query = window.matchMedia(NARROW_LAYOUT_BUILDER);
    const crossed = () => setShown(!query.matches);
    query.addEventListener("change", crossed);
    return () => query.removeEventListener("change", crossed);
  }, []);
  return [shown, setShown] as const;
}
