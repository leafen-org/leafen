import * as Y from "yjs";
import { api } from "@/api";
import { clearLegacyPageMutations, countLegacyPageMutations } from "@/lib/nativePageMutationLegacy";
import { forgetYgoRecovery, listYgoRecoveryLocalEdits } from "@/lib/ygoRecoveryLog";
import { pageNotYetMade } from "@/lib/pagesNotYetMade";
import type { Session } from "@/types";

// What sign-out finds and removes on this device besides the outbox: page
// text edited here that the server may not have yet, page changes from
// before the outbox, and the offline copies of the pages this account opened.

function base64Bytes(value: string): Uint8Array {
  const binary = atob(value);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

/**
 * Counts the pages whose text edits the server does not have yet, and the
 * page changes from before the outbox. A page the server cannot be asked
 * about fails the count, so it is never read as nothing.
 */
export async function readUnsyncedDeviceChangeCount(session: Session): Promise<number> {
  let count = await countLegacyPageMutations();
  for (const page of await listYgoRecoveryLocalEdits(session.user.id)) {
    if (page.updates.length === 0) continue;
    // A page made here the server hasn't made has none of its text there,
    // and isn't asked about until it is made (apiTransport.ts).
    if (await pageNotYetMade(page.pageId)) {
      count += 1;
      continue;
    }
    let serverStateVector: Uint8Array;
    try {
      serverStateVector = base64Bytes((await api.collaborationStateVector(session, page.pageId)).stateVector);
    } catch {
      // A page this account can no longer open keeps its edits here only, and
      // one the server can't be asked about may too (2026-11-05: offline, the
      // count failed and sign-out could only say it could not tell).
      count += 1;
      continue;
    }
    const missing = Y.decodeUpdate(Y.diffUpdate(Y.mergeUpdates(page.updates), serverStateVector));
    if (missing.structs.length > 0) count += 1;
  }
  return count;
}

/** Removes the account's page copies and old page changes from this device. */
export async function forgetDevicePageCopies(userId: string): Promise<void> {
  await Promise.all([forgetYgoRecovery(userId), clearLegacyPageMutations()]);
}
