import { ServerConnectConsent } from "@/components/sidebar/ServerConnectConsent";
import { LocalAgentRequests } from "@/components/mcp/LocalAgentRequests";
import { startTransition, Suspense, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { lazyScreen } from "@/lib/lazyScreen";
import { api } from "@/api";
import { AddAccountWindow } from "@/components/auth/AddAccountWindow";
import { AuthGate } from "@/components/auth/AuthGate";
import {
  authAddressSpent, authLinkWaitsWhileSignedIn, linkLeftWhileSignedIn, settleLinkLeftWhileSignedIn, signedInAuthAddress, type LinkLeftWhileSignedIn,
} from "@/components/auth/initialAuthEntry";
import { SignedInLinkNotice } from "@/components/auth/SignedInLinkNotice";
import { loadAuthenticatedSession } from "@/components/auth/authenticatedModules";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { ProductLoadingScreen } from "@/components/loading/ProductLoadingScreen";
import { SharedPageLoading } from "@/components/SharedPageLoading";
import { PageAnalyticsRuntimeProvider } from "@/lib/pageAnalyticsRuntime";
import { ProductToastProvider, useProductToast } from "@/lib/productToast";
import { SlowScreenLoadNotice } from "@/components/product/SlowScreenLoadNotice";
import { replaceRoute, routeInfoFromPath, usePathname } from "@/lib/router";
import { fetchRuntimeConfig, refreshRuntimeConfig } from "@/lib/runtimeConfigService";
import { retryWhenServerMayAnswer } from "@/lib/serverReach";
import { clearSessionIfCurrent, followSessionRenewals, followStoredSession, loadSession } from "@/lib/authSessionStore";
import { rememberReauthReturnPath } from "@/lib/reauthReturnPath";
import { markSignedOutByServer, signedOutByHand, signedOutByServer } from "@/lib/signOutMarks";
import { resetDocumentChrome } from "@/lib/documentChrome";
import { addingAccount, endAddingAccount, subscribeServerAccounts } from "@/lib/serverAccounts";
import { isSignedOutResponse, onSignedOutResponse } from "@/lib/signedOutResponse";
import { takeOtherAccountSessionNotice } from "@/lib/productSignOut";
import { forgetOwnServerSignOut } from "@/lib/ownServerSignOut";
import { accountLanguageSettled } from "@/lib/accountLanguageSettled";
import { onUpdateReady } from "@/lib/updateReadyNotice";
import {
  createNativeFormService,
  nativeFormSurfaceBounds,
} from "@/lib/nativeFormService";
import type { Session } from "@/types";

// The screens the address opens as the page loads, each drawn with its start and opened by no press (lazyScreen `entry`).
const AuthenticatedSession = lazyScreen(() => loadAuthenticatedSession().then((module) => ({
  default: module.AuthenticatedSession,
})), { entry: true });
const PasskeyBrowserPage = lazyScreen(() => import("@/components/auth/PasskeyBrowserPage").then((module) => ({
  default: module.PasskeyBrowserPage,
})), { entry: true });
const AppSignInBrowserPage = lazyScreen(() => import("@/components/auth/AppSignInBrowserPage").then((module) => ({
  default: module.AppSignInBrowserPage,
})), { entry: true });
const GuestAppView = lazyScreen(() => import("@/components/apps/GuestAppView").then((module) => ({ default: module.GuestAppView })), { entry: true });
const ViewLinkView = lazyScreen(() => import("@/components/ViewLinkView").then((module) => ({ default: module.ViewLinkView })), { entry: true });
const SharedPageView = lazyScreen(() => import("@/components/SharedPageView").then((module) => ({
  default: module.SharedPageView,
})), { entry: true });
const PublicSiteView = lazyScreen(() => import("@/components/PublicSiteView").then((module) => ({
  default: module.PublicSiteView,
})), { entry: true });
const PublicFormView = lazyScreen(() => import("@/components/PublicFormView").then((module) => ({
  default: module.PublicFormView,
})), { entry: true });

const passkeyBrowserEntry = window.location.pathname === "/auth/passkey";
// An app signing in through this browser (AppSignInBrowserPage).
const appSignInBrowserEntry = window.location.pathname === "/auth/app";

function ProductApp({ initialSession }: { initialSession: Session | null }) {
  const [session, setSession] = useState<Session | null>(initialSession);
  const heldSession = useRef(session);
  heldSession.current = session;
  // Whether this page has been signed in since it loaded: its sign-in screen
  // then isn't the one a page link brings someone to (2026-11-06: signing out
  // in another tab left this one saying "거의 다 됐습니다! …").
  const hadSession = useRef(false);
  if (session) hadSession.current = true;
  const [sessionEnded, setSessionEnded] = useState(false);
  // A members' form opened signed out: the sign-in screen shows at the
  // form's address, naming its workspace, and comes back to it (reauthReturnPath.ts).
  const [formSignIn, setFormSignIn] = useState(false);
  useEffect(() => { if (session) setFormSignIn(false); }, [session]);
  const pathname = usePathname();
  const route = routeInfoFromPath(pathname);
  // An auth link opened while signed in (AuthGate.tsx): its step shows here
  // until it's done or left; reached inside the app, the page starts again to
  // read its link, and a device code or setup link goes home.
  const [authLink, setAuthLink] = useState(() => !!initialSession && authLinkWaitsWhileSignedIn());
  // Someone else's invitation or device link, which this signed-in browser
  // doesn't use: said, not dropped — once the server still takes the stored
  // sign-in. One it no longer takes signs out and the sign-in screen uses the
  // link (2026-10-28: an ended sign-in dropped the invitation with it). It is
  // taken or said, and leaves the address, once the page reads in the
  // account's language, which a reload may bring (lib/accountLanguageSettled.ts);
  // unsaid, it goes with the tab through that reload (initialAuthEntry.ts).
  // The notice belongs to the sign-in it was said to, gone with it.
  const [leftLink, setLeftLink] = useState<LinkLeftWhileSignedIn | null>(null);
  useEffect(() => {
    const link = initialSession ? linkLeftWhileSignedIn() : null;
    if (!initialSession || !link) return undefined;
    let current = true;
    void api.listAuthSessions(initialSession).then(() => true, (caught: unknown) => {
      const failure = caught as { status?: unknown; code?: unknown } | null;
      return !isSignedOutResponse(failure?.status, failure?.code);
    }).then(async (live) => {
      if (live) await accountLanguageSettled();
      if (!current || !live) return;
      settleLinkLeftWhileSignedIn();
      setLeftLink(link);
    });
    return () => { current = false; };
  }, [initialSession]);
  useEffect(() => { if (!session) setLeftLink(null); }, [session]);
  useEffect(() => {
    if (!session || authLink) return;
    const action = signedInAuthAddress(pathname);
    if (action === "home" || (action === "gate" && authAddressSpent(pathname))) replaceRoute("/");
    else if (action === "gate") window.location.reload();
  }, [authLink, pathname, session]);
  useEffect(() => { if (!session) setAuthLink(false); }, [session]);
  // Another account signing in beside this one, in a window over the page; a
  // provider's way back to a new page goes on as an auth link does.
  const adding = useSyncExternalStore(subscribeServerAccounts, addingAccount);
  useEffect(() => { if (!session) endAddingAccount(); }, [session]);
  // Another account signing in closes the notice that an earlier one's
  // server session stayed open (productSignOut.ts).
  const { dismissErrors, dismissMessage } = useProductToast();
  useEffect(() => {
    const message = session ? takeOtherAccountSessionNotice(session.user.id) : null;
    if (message) dismissMessage(message);
  }, [dismissMessage, session]);
  // A sign-in that ends takes its failures with it: what stopped working as it
  // ended isn't said over the sign-in screen (2026-11-06: "댓글을 불러오지
  // 못했습니다" stayed up after another device ended the session).
  useEffect(() => { if (!session) dismissErrors(); }, [dismissErrors, session]);
  const formService = useMemo(
    () => createNativeFormService(api, () => session),
    [session],
  );

  // A sign-in renewed on a request, here or in another tab, carries on.
  useEffect(() => (session ? followSessionRenewals(session) : undefined), [session]);

  useEffect(() => followStoredSession(() => heldSession.current, (next) => {
    const session = heldSession.current;
    // A sign-out asked for in another tab keeps no place here either.
    if (session && !next && signedOutByHand(session)) replaceRoute("/");
    else if (session) rememberReauthReturnPath(session.user.id);
    if (session && next && session.user.id !== next.user.id) {
      resetDocumentChrome();
      replaceRoute("/");
    }
    // One the server ended, which another tab learned first, is said here too.
    setSessionEnded(Boolean(session && !next && signedOutByServer(session)));
    setSession(next);
  }), []);

  // The server no longer takes this browser's sign-in: the sign-in screen
  // says so, and signing in as the same person opens this place again. The
  // changes this device holds for the account stay to be sent then. One the
  // person signed out of in another tab keeps no place, as above, and the
  // next sign-in opens the start page: a tab of the app holds its own sign-in
  // and learns of that sign-out only from this answer.
  useEffect(() => {
    if (!session) return undefined;
    return onSignedOutResponse((token) => { void (async () => {
      if (token !== session.token) return;
      const byHand = signedOutByHand(session);
      if (!byHand) { rememberReauthReturnPath(session.user.id); markSignedOutByServer(session); }
      await clearSessionIfCurrent(session, true);
      if (heldSession.current?.token !== session.token) return;
      // A sign-in held here instead goes on. None held, it ended whoever
      // cleared it: another tab that learned it first may have, its clear
      // landing before this tab heard of it (2026-11-14, more often without
      // Web Locks, where the clears queue in IndexedDB), and the end is said
      // all the same.
      const replacement = loadSession();
      if (replacement) {
        setSessionEnded(false);
        if (replacement.user.id !== session.user.id) { resetDocumentChrome(); replaceRoute("/"); }
        setSession(replacement);
        return;
      }
      // Left at once with the product: a root it still showed would open home.
      if (byHand) replaceRoute("/");
      setSessionEnded(!byHand);
      setSession(null);
    })().catch(() => {
      if (heldSession.current?.token !== session.token) return;
      setSessionEnded(true);
      setSession(null);
    }); });
  }, [session]);

  if (route.kind === "share") {
    return <Suspense fallback={<SharedPageLoading />}><SharedPageView token={route.shareId} session={session} /></Suspense>;
  }
  if (route.kind === "view") {
    return <Suspense fallback={<SharedPageLoading />}><ViewLinkView /></Suspense>;
  }
  if (route.kind === "appGuest") {
    return <Suspense fallback={<SharedPageLoading />}><GuestAppView /></Suspense>;
  }
  // Signed in, a form's link opens as a page of the app (MemberFormPage).
  if (route.kind === "form" && !session && !formSignIn) {
    return (
      <Suspense fallback={<SharedPageLoading />}>
        <PublicFormView
          bounds={nativeFormSurfaceBounds}
          service={formService}
          token={route.formToken}
          onSignIn={() => setFormSignIn(true)}
        />
      </Suspense>
    );
  }
  if (route.kind === "site") {
    return <Suspense fallback={<SharedPageLoading />}><PublicSiteView slug={route.siteSlug} /></Suspense>;
  }
  if (session && authLink) {
    return <AuthGate signedInAs={session.user.id} addingAccount={adding} onReturn={() => {
      endAddingAccount();
      setAuthLink(false);
      replaceRoute("/");
    }} onSession={(next) => {
      endAddingAccount();
      setAuthLink(false);
      setSessionEnded(false);
      setSession(next);
    }} />;
  }
  // Signed out and signed in share one boundary, and signing in is a
  // transition: the sign-in or setup screen stays up until the signed-in app's
  // code is there, instead of the loading screen's blank moment between a
  // first run's steps (2026-11-06, the name step to the language step).
  if (!session) {
    return (
      <Suspense fallback={<ProductLoadingScreen source="workspace" />}>
        <AuthGate sessionEnded={sessionEnded} pageWall={!hadSession.current} onSession={(next) => {
          setSessionEnded(false);
          // A sign-in ends the mark of a sign-out asked for on the app's own server.
          forgetOwnServerSignOut();
          startTransition(() => setSession(next));
        }} />
      </Suspense>
    );
  }

  return (
    <Suspense fallback={<ProductLoadingScreen source="workspace" />}>
      <AuthenticatedSession key={session.user.id} session={session} onSession={setSession} />
      {leftLink ? <SignedInLinkNotice session={session} link={leftLink} onClose={() => setLeftLink(null)} /> : null}
      {adding ? <AddAccountWindow onSession={(next) => {
        endAddingAccount();
        setSessionEnded(false);
        if (next.user.id !== session.user.id) resetDocumentChrome();
        setSession(next);
      }} /> : null}
      <ServerConnectConsent session={session} />
      <LocalAgentRequests session={session} />
    </Suspense>
  );
}

function RuntimeConfigLifecycle() {
  const { notify } = useProductToast();

  useEffect(() => onUpdateReady(({ message, reloadLabel, reload }) => (
    // It stays until taken: one who comes back to the tab still sees it.
    notify(message, "default", { label: reloadLabel, onClick: reload, autoDismissMs: null })
  )), [notify]);

  useEffect(() => {
    let mounted = true;
    let stopRetry = () => {};
    void fetchRuntimeConfig().then((runtimeConfig) => {
      if (!mounted) return;
      // With the server unreachable, its settings are read again once it may answer.
      if (runtimeConfig.unreachable) {
        stopRetry = retryWhenServerMayAnswer(() => {
          void refreshRuntimeConfig().then((next) => {
            if (!next.unreachable) stopRetry();
          });
        });
        return;
      }
      if (runtimeConfig.degradation) notify(runtimeConfig.degradation.message, "error");
    });
    return () => {
      mounted = false;
      stopRetry();
    };
  }, [notify]);

  return null;
}

export default function App({ initialSession }: { initialSession: Session | null }) {
  return (
    <ErrorBoundary scope="root" reloadOnRetry>
      <PageAnalyticsRuntimeProvider>
        <ProductToastProvider>
          <RuntimeConfigLifecycle />
          <SlowScreenLoadNotice />
          {passkeyBrowserEntry
            ? <Suspense fallback={<ProductLoadingScreen source="auth" />}><PasskeyBrowserPage initialSession={initialSession} /></Suspense>
            : appSignInBrowserEntry
              ? <Suspense fallback={<ProductLoadingScreen source="auth" />}><AppSignInBrowserPage initialSession={initialSession} /></Suspense>
              : <ProductApp initialSession={initialSession} />}
        </ProductToastProvider>
      </PageAnalyticsRuntimeProvider>
    </ErrorBoundary>
  );
}
