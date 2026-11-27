import { desktopBackgroundAvailable } from "./desktopBackground";
import { watchKeychainPrompt } from "./keychainPrompt";

// The desktop app's network proxy (desktop/network_proxy_desktop.go): the
// system's and the environment's unless the person sets one in This
// computer, its password kept in the computer's secret storage.

export type NetworkProxyInForce = {
  source: "" | "leafen" | "environment" | "system";
  http?: string;
  https?: string;
  noProxy?: string;
  /** The system's automatic configuration script it follows, or one the network gives. */
  autoConfig?: string;
  autoDiscover?: boolean;
  unfollowed?: "" | "autoconfig" | "socks";
};

export type DesktopNetworkProxyState = {
  mode: "system" | "manual";
  url: string;
  noProxy: string;
  username: string;
  passwordSet: boolean;
  inForce: NetworkProxyInForce;
};

export type NetworkProxyInput = {
  mode: "system" | "manual";
  url: string;
  noProxy: string;
  username: string;
  password?: string;
  clearPassword?: boolean;
};

async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const { Call } = await import("@wailsio/runtime");
  // Its password's read may wait on macOS's prompt, which the window says (keychainPrompt.ts).
  const asking = async () => await Call.ByName("main.CredentialBroker.Asking") === true;
  return await watchKeychainPrompt(Call.ByName(`main.NetworkProxy.${method}`, ...args), asking) as T;
}

/** Whether this window sets its computer's proxy: the desktop app's. */
export const desktopNetworkProxyAvailable = desktopBackgroundAvailable;

export const desktopNetworkProxy = {
  state: () => call<DesktopNetworkProxyState>("State"),
  set: (input: NetworkProxyInput) => call<DesktopNetworkProxyState>("Set", input),
};
