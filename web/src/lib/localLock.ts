import {
  createBrowserLocalLockGateRuntime,
  type LocalBoxGate,
  type LocalEncryptionMode,
} from "./localLockGate";
import {
  changeLocalPassphraseBox,
  createLocalPassphraseBox,
  removeLocalPassphraseBox,
  type LocalPassphraseBoxOptions,
} from "./localPassphraseBox";
import type { LocalSecretBox } from "./localSecretBox";

export type { LocalBoxGate, LocalEncryptionMode } from "./localLockGate";

const gate = createBrowserLocalLockGateRuntime();

export function localEncryptionMode(): LocalEncryptionMode {
  return gate.localEncryptionMode();
}

export function setLocalEncryptionMode(mode: LocalEncryptionMode): void {
  gate.setLocalEncryptionMode(mode);
}

export function lockBoxName(userId: string): string {
  return `leafen-lock:${userId}`;
}

export function onLocalEncryptionModeChange(
  listener: () => void,
): () => void {
  return gate.onModeChange(listener);
}

export function awaitLocalBox(userId: string): Promise<LocalBoxGate> {
  return gate.awaitLocalBox(userId);
}

export function localBoxIfSettled(userId: string): LocalBoxGate | "pending" {
  return gate.localBoxIfSettled(userId);
}

export function localLockPending(userId: string): boolean {
  return gate.localLockPending(userId);
}

export async function unlockLocalData(
  userId: string,
  passphrase: string,
  options?: Pick<LocalPassphraseBoxOptions, "iterations">,
): Promise<"ok" | "unavailable" | "wrong-passphrase"> {
  const result = await createLocalPassphraseBox(
    lockBoxName(userId),
    passphrase,
    options,
  );
  if ("error" in result) return result.error;
  gate.prime(userId, result.box);
  return "ok";
}

export async function changeLocalDataPassphrase(
  userId: string,
  currentPassphrase: string,
  nextPassphrase: string,
  options?: Pick<LocalPassphraseBoxOptions, "onFailure">,
): Promise<ReturnType<typeof changeLocalPassphraseBox> extends Promise<infer Result> ? Result : never> {
  return changeLocalPassphraseBox(
    lockBoxName(userId),
    currentPassphrase,
    nextPassphrase,
    options,
  );
}

export function createLocalDataPassphraseBox(
  userId: string,
  passphrase: string,
  options?: Pick<LocalPassphraseBoxOptions, "onFailure">,
) {
  return createLocalPassphraseBox(lockBoxName(userId), passphrase, options);
}

export function removeLocalDataPassphraseBox(userId: string): Promise<void> {
  return removeLocalPassphraseBox(lockBoxName(userId));
}

export function skipLocalLock(userId: string): void {
  gate.skip(userId);
}

export function primeUnlockedGate(userId: string, box: LocalSecretBox): void {
  gate.prime(userId, box);
}

export function resetGateToDevice(userId: string): void {
  gate.resetToDevice(userId);
}
