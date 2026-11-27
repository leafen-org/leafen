import * as Y from "yjs";
import { api } from "@/api";
import type { Session } from "@/types";

function base64Bytes(value: string): Uint8Array {
  const binary = atob(value);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

/** Whether a document holds anything the server's stored copy doesn't: text,
 * or deletions — the server decides, given only what it is missing. */
export async function serverLacks(session: Session, pageId: string, update: Uint8Array): Promise<boolean> {
  const stateVector = base64Bytes((await api.collaborationStateVector(session, pageId)).stateVector);
  return (await api.collaborationStateLacks(session, pageId, Y.diffUpdate(update, stateVector))).lacks;
}
