import { Suspense, useEffect, useLayoutEffect, useMemo, useState } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import { isPackagedClient } from "@/lib/packagedClient";
import type { GraphOperation } from "@/contentGraph";
import type { Session } from "@/types";
import { createNativePageMutationOutboxBrowser } from "@/lib/nativePageMutationBrowser";
import { graphWriteWaitsForServer } from "@/lib/nativePageMutationFailure";
import { blockChangeTextOf } from "@/lib/blockChangeText";
import { keepContentGraphOutbox, sendContentGraphChangeNow } from "@/lib/contentGraphSend";
import { keptChangeRoutes } from "@/lib/keptChangeRoutes";
import { sendKeptPageMetadata } from "@/lib/nativePageMetadata";
import { sendPageText } from "@/lib/keptChangeTaken";
import { sendPageCreationHere } from "@/lib/pageCreationService";
import { localLockPending } from "@/lib/localLock";
import { tellKeptChanges } from "@/lib/pageTextBehindKeptChanges";
import { keepTypedDrafts, recoverTypedDrafts, withTypedDraftsDiscarded } from "@/lib/typedDraftSender";
import { AuthenticatedAccountGates } from "./AuthenticatedAccountGates";
import LocalLockGate from "../LocalLockGate";
import { ProductLoadingScreen } from "../loading/ProductLoadingScreen";
import { ServerRestoredDialog } from "../product/ServerRestoredDialog";
import { useCommittedResourceDisposal } from "../product/useCommittedResourceDisposal";
import { loadAuthenticatedProduct } from "./authenticatedModules";
import { useSessionProfileSync } from "./useSessionProfileSync";

const AuthenticatedProduct = lazyScreen(loadAuthenticatedProduct, { entry: true });
/** Fetches the product's code ahead, so a screen handing over to it draws it in the same pass (lib/firstAdministratorStart.ts). */
export const preloadAuthenticatedProduct = () => AuthenticatedProduct.preload();
const PackagedLinkOpener = lazyScreen(() => import("@/components/sidebar/PackagedLinkOpener").then((module) => ({ default: module.PackagedLinkOpener })), { standIn: () => null, entry: true });

/** One account owns the outbox even while restored data requires account setup. */
export function AuthenticatedSession({ session, onSession }: {
  session: Session;
  onSession(session: Session | null): void;
}) {
  const outbox = useMemo(() => withTypedDraftsDiscarded(session.user.id, createNativePageMutationOutboxBrowser({
    // A page's text taken already, its answer lost, is answered as taken (lib/keptChangeTaken.ts).
    send: (input, signal) => sendPageText(session, input, signal),
    answeredCursor: (result) => result.receipt.cursor,
    // A database start the server makes goes by its own route (databaseStartKept.ts), loaded when one is sent;
    // so do rows going to the trash or out of it (rowTrashKept.ts), a row's values written by its page's route (rowValuesKept.ts) and a row's page moved (rowMoveKept.ts),
    // each taken at once when its module is loaded already (keptChangeRoutes.ts).
    sendGraph: ({ mutationId, operations, pageId, words, typing, pageKind, replacesPage, start, rowTemplates, rowTrash, rowValues, rowMove }, signal) => start
      ? (keptChangeRoutes.start?.(session, { mutationId, pageId, start }, signal)
        ?? import("@/lib/databaseStartKept").then(({ sendKeptDatabaseStart }) => sendKeptDatabaseStart(session, { mutationId, pageId, start }, signal)))
      : rowTrash ? (keptChangeRoutes.rowTrash?.(session, { mutationId, rowTrash }, signal)
        ?? import("@/lib/rowTrashKept").then(({ sendKeptRowTrash }) => sendKeptRowTrash(session, { mutationId, rowTrash }, signal)))
      : rowValues ? (keptChangeRoutes.rowValues?.(session, { mutationId, rowValues }, signal)
        ?? import("@/lib/rowValuesKept").then(({ sendKeptRowValues }) => sendKeptRowValues(session, { mutationId, rowValues }, signal)))
      : rowMove ? (keptChangeRoutes.rowMove?.(session, { mutationId, rowMove }, signal)
        ?? import("@/lib/rowMoveKept").then(({ sendKeptRowMove }) => sendKeptRowMove(session, { mutationId, rowMove }, signal)))
      : sendContentGraphChangeNow(session, pageId, { mutationId, operations: operations as GraphOperation[], ...(pageKind ? { pageKind } : {}),
        ...(pageKind && replacesPage ? { replacesPage } : {}), ...blockChangeTextOf({ words, typing }), ...(rowTemplates ? { rowTemplates } : {}) }, signal),
    sendMetadata: (input, signal) => sendKeptPageMetadata(session, input, signal),
    sendPageCreation: (input, signal) => sendPageCreationHere(session, input, signal),
    userId: session.user.id, waitsForServer: graphWriteWaitsForServer,
    // What a tab gone kept as it was typed goes in as the store opens (lib/typedDraftSender.ts).
    recoverTyped: (store) => recoverTypedDrafts(session.user.id, store),
  })), [session]);
  useCommittedResourceDisposal(outbox);
  // Each key typed in a title-like field is kept on this device for the account (lib/typedDraftSender.ts).
  useLayoutEffect(() => keepTypedDrafts(session.user.id), [session.user.id]);
  // The outbox in use holds what asks about the pages it makes until made:
  // an outbox this memo made for a screen drawn and thrown away holds none
  // (lib/nativeGraphPagesWaiting.ts). Before any part's own first reads.
  useLayoutEffect(() => outbox.tellPagesNotYetMade(), [outbox]);
  // A page's text kept here goes behind the changes it keeps (lib/pageTextBehindKeptChanges.ts).
  useLayoutEffect(() => tellKeptChanges(outbox), [outbox]);
  // Every change to a page's content this account makes waits in it (lib/contentGraphSend).
  useEffect(() => keepContentGraphOutbox(session.user.id, outbox), [outbox, session.user.id]);
  useSessionProfileSync(session, onSession);
  // The product reads its copies as it opens, and those of a lock still closed
  // can't be read: unlocked, it opens anew with them (2026-11-04: unlocking
  // away from the server left an empty workspace).
  const [unlocks, setUnlocks] = useState(0);
  // A lock still closed is asked about first, alone: what the server's new
  // data means for this device's changes waits for it, and can't count them
  // before (2026-11-06: both windows came up at once, the count "being worked out").
  const [lockSettled, setLockSettled] = useState(() => !localLockPending(session.user.id));
  // One boundary over the account's steps and the product: a step that ends
  // (the language) stays up until the product's code is there, instead of the
  // loading screen's blank moment between them (2026-11-06).
  return <>
    <Suspense fallback={<ProductLoadingScreen source="workspace" />}>
      <AuthenticatedAccountGates session={session} onSession={onSession}>
        <LocalLockGate userId={session.user.id} onUnlocked={() => setUnlocks((count) => count + 1)} onSettled={() => setLockSettled(true)} />
        {/* Behind a lock still closed the product isn't drawn: it can't read its
            copies yet and said "워크스페이스 없음" there (2026-11-06); unlocked or
            skipped, it opens as the lock leaves it. */}
        {lockSettled ? <AuthenticatedProduct key={unlocks} session={session} onSession={onSession} pageMutationOutbox={outbox} />
          : <ProductLoadingScreen source="workspace" />}
      </AuthenticatedAccountGates>
    </Suspense>
    <ServerRestoredDialog mutationOutbox={outbox} session={session} held={!lockSettled} />
    {/* The app's links, taken here while signed in (another server's sign-in and the first start take their own). */}
    {isPackagedClient() ? <Suspense fallback={null}><PackagedLinkOpener /></Suspense> : null}
  </>;
}
