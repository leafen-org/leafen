import { lazyScreen } from "@/lib/lazyScreen";

/**
 * A database page's own screen (StandaloneRowPage), one lazy part for every
 * place that draws it — its address, a peek, a wiki's page — fetched once the
 * app is idle. Its code that came for one place is there for the next in the
 * same pass: a part of each place's own, a wiki's page opened whole from its
 * peek waited for its code again before it drew (2026-11-17, a NAS-class
 * server); opened whole from its peek it draws at once (rowPageHandoff.ts).
 */
export const StandaloneRowPageScreen = lazyScreen(() => import("./StandaloneRowPage")
  .then((module) => ({ default: module.StandaloneRowPage })), { typedOnOpen: true });

/**
 * A database page's place while its address is asked about (RowPageReading):
 * its code comes with the page's own, so a page drawn here before draws its
 * path there in the same pass.
 */
export const RowPageReadingScreen = lazyScreen(() => import("./RowPageReading")
  .then((module) => ({ default: module.RowPageReading })), { typedOnOpen: true });
