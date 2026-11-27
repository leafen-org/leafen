import { withLoadDeadline } from "@/lib/lazyScreen";
/**
 * The signed-in product's code, downloaded only once someone is signed in, so
 * the sign-in screen and public pages never carry it. The session layer and
 * the product start downloading together rather than one after the other; a
 * download that fails is tried again the next time it is asked for.
 */
function loadOnce<T>(load: () => Promise<T>) {
  let pending: Promise<T> | null = null;
  return () => (pending ??= load().catch((error: unknown) => {
    pending = null;
    throw error;
  }));
}

// Bounded as a screen's code is (lib/lazyScreen.ts), so a stalled load is asked again.
export const loadAuthenticatedProduct = loadOnce(() => withLoadDeadline(import("../product/AuthenticatedProduct")));
const loadSessionModule = loadOnce(() => withLoadDeadline(import("./AuthenticatedSession")));

export function loadAuthenticatedSession() {
  void loadAuthenticatedProduct().catch(() => undefined);
  return loadSessionModule();
}
