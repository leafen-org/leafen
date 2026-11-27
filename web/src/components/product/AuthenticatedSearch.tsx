import { type ComponentProps, Suspense, useEffect } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import { closeSearch, useSearchOpen } from "@/lib/searchOpen";

const SearchDialog = lazyScreen(() => import("@/components/search/SearchDialog").then((module) => ({ default: module.SearchDialog })), { typedOnOpen: true });

/**
 * Search (⌘K) over the product: opened and closed through lib/searchOpen.ts,
 * which this alone follows, so that its press draws the window and nothing
 * of the product beneath.
 */
export function AuthenticatedSearch(props: Omit<ComponentProps<typeof SearchDialog>, "open" | "onClose">) {
  const open = useSearchOpen();
  // A product left (another account, signed out) leaves its search shut.
  useEffect(() => closeSearch, []);
  return <Suspense fallback={null}><SearchDialog {...props} open={open} onClose={closeSearch} /></Suspense>;
}
