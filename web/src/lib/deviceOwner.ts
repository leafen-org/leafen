// Signing back in to the app's own server by the device's owner check
// (desktop/device_owner.go): the system checks the owner — Touch ID, Face ID
// or the device's password — and the shell makes a one-time device code in
// the server, which the window joins with. Only the app's page reaches it. A
// sign-in the window lost without its person signing out needs no check: the
// person who set the server up is signed back in by the window itself.

export type DeviceOwnerAccount = { id: string; name: string };

export type DeviceOwnerSignIn =
  | { outcome: "code"; code: string }
  | { outcome: "cancelled" | "unavailable" }
  // The account's organization keeps it from device sign-ins (its error code).
  | { outcome: "refused"; reason: string };

async function call(name: string, ...args: unknown[]): Promise<unknown> {
  const { Call } = await import("@wailsio/runtime");
  return Call.ByName(`main.DeviceOwner.${name}`, ...args);
}

/** Whether this device can check its owner; false where the app has no such check. */
export async function deviceOwnerAvailable(): Promise<boolean> {
  try {
    return (await call("Available")) === true;
  } catch {
    return false;
  }
}

export async function deviceOwnerAccounts(): Promise<DeviceOwnerAccount[]> {
  const value = await call("Accounts");
  return Array.isArray(value)
    ? value.filter((item): item is DeviceOwnerAccount => typeof item?.id === "string" && typeof item?.name === "string")
    : [];
}

function signInFrom(value: { outcome?: unknown; code?: unknown; reason?: unknown } | null): DeviceOwnerSignIn {
  if (value?.outcome === "code" && typeof value.code === "string") return { outcome: "code", code: value.code };
  if (value?.outcome === "cancelled") return { outcome: "cancelled" };
  if (value?.outcome === "refused") return { outcome: "refused", reason: typeof value.reason === "string" ? value.reason : "" };
  return { outcome: "unavailable" };
}

export async function signInAsDeviceOwner(accountId: string, reason: string): Promise<DeviceOwnerSignIn> {
  try {
    return signInFrom(await call("SignIn", accountId, reason) as { outcome?: unknown } | null);
  } catch {
    // The app can't check here.
    return { outcome: "unavailable" };
  }
}

/**
 * A code for the person who set the app's own server up, with no system
 * check: the window landed on its sign-in page without that person having
 * signed out there (DeviceOwner.SignInBack).
 */
export async function signInBackAsServerOwner(): Promise<DeviceOwnerSignIn> {
  try {
    return signInFrom(await call("SignInBack") as { outcome?: unknown } | null);
  } catch {
    // An app without it, or its server off.
    return { outcome: "unavailable" };
  }
}
