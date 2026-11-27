import { useEffect, useState } from "react";
import type { TFunction } from "i18next";
import { deviceOwnerAccounts, deviceOwnerAvailable, signInAsDeviceOwner, signInBackAsServerOwner, type DeviceOwnerAccount } from "@/lib/deviceOwner";
import { ownServerSignedOut } from "@/lib/ownServerSignOut";
import { authWords, type AuthText } from "@/lib/authLine";

export type DeviceOwnerSignInState = {
  /** The window is on the app's own server, where the owner check signs in. */
  local: boolean;
  /** Whether the device can check its owner; null while asked. */
  available: boolean | null;
  /** The accounts to choose from when there is more than one. */
  accounts: DeviceOwnerAccount[] | null;
  start(): void;
  choose(accountId: string): void;
  /**
   * The window landed on the sign-in page of its own server, signed out:
   * unless the person signed out there, the person who set that server up
   * is signed back in at once (ownServerSignOut.ts).
   */
  signInBack(): void;
};

// One sign-in back at a time, across the sign-in page's mounts.
let signingBack = false;

/**
 * "이 기기로 들어가기" on the app's own server's sign-in page
 * (desktop/device_owner.go): one account signs in at once, several are
 * chosen by name; the code the shell makes joins as any device code does.
 * Landing there without having signed out, the window signs the person who
 * set the server up back in by itself (signInBack).
 */
export function useDeviceOwnerSignIn({ local, joinWithCode, setError, t }: {
  local: boolean;
  joinWithCode(code: string): Promise<void>;
  setError(message: AuthText): void;
  t: TFunction;
}): DeviceOwnerSignInState {
  const [available, setAvailable] = useState<boolean | null>(local ? null : false);
  const [accounts, setAccounts] = useState<DeviceOwnerAccount[] | null>(null);

  useEffect(() => {
    if (!local) return undefined;
    let current = true;
    void deviceOwnerAvailable().then((value) => { if (current) setAvailable(value); });
    return () => { current = false; };
  }, [local]);

  async function choose(accountId: string) {
    setError("");
    const answer = await signInAsDeviceOwner(accountId, t("authGate:deviceOwnerReason"));
    if (answer.outcome === "code") {
      setAccounts(null);
      await joinWithCode(answer.code);
    } else if (answer.outcome === "refused") {
      setError(authWords(answer.reason === "LEAFEN_SAML_REQUIRED" ? "productErrors:correctable.organizationSSO"
        : answer.reason === "LEAFEN_DEVICE_SIGN_IN_RESTRICTED" ? "productErrors:correctable.organizationPasswordTwoStep"
          : "authGate:deviceOwnerRefused"));
    } else if (answer.outcome === "unavailable") setError(authWords("authGate:deviceOwnerUnavailable"));
  }

  async function start() {
    setError("");
    try {
      const found = await deviceOwnerAccounts();
      if (found.length === 1) await choose(found[0].id);
      else if (found.length === 0) setError(authWords("authGate:deviceOwnerNoAccounts"));
      else setAccounts(found);
    } catch {
      setError(authWords("authGate:deviceOwnerUnavailable"));
    }
  }

  async function signInBack() {
    if (!local || signingBack || ownServerSignedOut()) return;
    signingBack = true;
    try {
      const answer = await signInBackAsServerOwner();
      // Otherwise the page stays, the owner check its way back.
      if (answer.outcome === "code") await joinWithCode(answer.code);
    } finally {
      signingBack = false;
    }
  }

  return { local, available, accounts, start: () => void start(), choose: (id) => void choose(id), signInBack: () => void signInBack() };
}
