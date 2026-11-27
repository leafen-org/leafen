import type { SettingsWindowState } from "./settingsWindowModel";

const SETTINGS_WINDOW_REQUEST = "leafen:settings-window-request";

/**
 * Opens the settings window over the current screen from a screen that has
 * no hold of it (an app's stopped notice, the apps page): the window keeps
 * the address and gives focus back to what opened it when it closes, as
 * the sidebar's settings do, not a trip to a settings address and back. An
 * opener its screen draws again as the window closes (the app's state read
 * again) carries `data-focus-return`, so the focus finds the new one.
 */
export function requestSettingsWindow(state: SettingsWindowState) {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent<SettingsWindowState>(SETTINGS_WINDOW_REQUEST, { detail: state }));
}

export function onSettingsWindowRequest(listener: (state: SettingsWindowState) => void): () => void {
  if (typeof window === "undefined") return () => {};
  const handle = (event: Event) => listener((event as CustomEvent<SettingsWindowState>).detail);
  window.addEventListener(SETTINGS_WINDOW_REQUEST, handle);
  return () => window.removeEventListener(SETTINGS_WINDOW_REQUEST, handle);
}
