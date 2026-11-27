import type { LocalSecretBox } from "./localSecretBox";

// A change written down as the page goes, sealed under the passphrase lock. A
// sealed box's bytes are put as text to lie in localStorage; its mutation says
// nothing of what was written and tells which one was answered.

export type SealedLeavingChange = { kind: "sealed"; mutationId: string; envelope: string };

// A step at a time: a large change (a pasted table) spread whole into one
// call overran the call stack, and the change couldn't be written down.
function bytesToText(bytes: Uint8Array) {
  let binary = "";
  for (let at = 0; at < bytes.length; at += 0x8000) binary += String.fromCharCode(...bytes.subarray(at, at + 0x8000));
  return btoa(binary);
}

function textToBytes(text: string): Uint8Array<ArrayBuffer> {
  const binary = atob(text);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
  return bytes;
}

export async function sealLeavingChange(
  box: LocalSecretBox,
  context: string,
  mutationId: string,
  change: unknown,
): Promise<SealedLeavingChange> {
  const sealed = await box.seal(change, context);
  return {
    envelope: JSON.stringify(sealed, (_, value: unknown) => (
      value instanceof Uint8Array ? { bytes: bytesToText(value) } : value
    )),
    kind: "sealed",
    mutationId,
  };
}

export function openLeavingChange<Change>(
  box: LocalSecretBox,
  context: string,
  record: SealedLeavingChange,
): Promise<Change | undefined> {
  const stored: unknown = JSON.parse(record.envelope, (_, value: unknown) => {
    const bytes = value && typeof value === "object" ? Reflect.get(value, "bytes") : undefined;
    return typeof bytes === "string" ? textToBytes(bytes) : value;
  });
  return box.open<Change>(stored, context);
}

export function leavingMutationId(record: SealedLeavingChange | { input: { mutationId: string } }): string {
  return "input" in record ? record.input.mutationId : record.mutationId;
}
