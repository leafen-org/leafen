import { serverOnThisComputer } from "./deviceFolder";

// The desktop app's own updater (desktop/updater.go): whether it looks for
// newer versions, whether the environment decides that, whether a store
// updates the app instead (the Microsoft Store's), and whether the person
// installs a newer version themselves (`manual`: the Windows zip, which only
// says one is out, 2026-11-17). A store's app and the zip leave the window no
// switch to draw — nor to find in the settings search.

export type DesktopUpdaterState = { enabled: boolean; managed: boolean; store?: boolean; manual?: boolean };

let known: DesktopUpdaterState | null = null;

async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const { Call } = await import("@wailsio/runtime");
  return await Call.ByName(`main.Updater.${method}`, ...args) as T;
}

export const desktopUpdater = {
  state: async () => (known = await call<DesktopUpdaterState>("Enabled")),
  setEnabled: (enabled: boolean) => call<void>("SetEnabled", enabled),
};

/**
 * Whether the app's update switch is drawn: in the desktop app on its own
 * server, when it updates itself — never on a phone, whose store updates it
 * (2026-11-21: a phone's own server found the switch in the settings search).
 */
export function desktopUpdateSwitchShown(): boolean {
  return serverOnThisComputer() && known?.store !== true && known?.manual !== true;
}

// Asked once the settings load, before a search could find the switch.
if (serverOnThisComputer()) void desktopUpdater.state().catch(() => undefined);
