import { isPackagedClient } from "./packagedClient";

/**
 * Whether this page can start its server again itself: the desktop and
 * phone apps run their server inside the app, which starts it again on the
 * same data when asked (a Leafen server elsewhere starts itself again).
 */
export function canRestartPackagedServer(): boolean {
  return isPackagedClient();
}

/** Starts the app's own server again and resolves once it answers again. */
export async function restartPackagedServer(): Promise<void> {
  const { Call } = await import("@wailsio/runtime");
  await Call.ByName("main.ServerRestarter.Restart");
}
