import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ConfirmCard } from "@/components/ConfirmActionDialog";
import { listenForPackagedLinks, packagedLinkTarget, type PackagedLinkTarget } from "@/lib/packagedLinks";
import { askBrowserSignIn, linkAsksBrowserSignIn } from "@/lib/openInApp";
import { addServerProfile, listProfiles, profileAt, reachedAt } from "@/lib/profileRegistry";
import { openProfile } from "@/lib/profileSwitch";
import { returnAfterSignInTo } from "@/lib/reauthReturnPath";
import { pushRoute } from "@/lib/router";
import { serverIdentity, serverOrigin } from "@/lib/serverOrigin";

// A link to a sign-in screen's own step (an invitation, a device code, a
// reset or email check, a provider's return, setup) opens as that step, as
// the address loaded afresh does (2026-10-26: a page invitation opened in the
// signed-out app stayed on sign-in); any other place waits for after signing in.
function signInStepPath(path: string): boolean {
  const address = new URL(path, "http://leafen.invalid");
  return /(?:^|&)invitation=/u.test(address.hash.slice(1))
    || /^\/(?:device\/|setup\/?$|reset-password\/?$|verify-email\/?$|auth\/(?:callback|saml))/u.test(address.pathname);
}

function openSignInStep(path: string) {
  if (new URL(path, window.location.href).pathname === window.location.pathname) {
    window.history.replaceState(null, "", path);
    window.location.reload();
  } else {
    window.location.assign(path);
  }
}

function hostOf(origin: string): string {
  try {
    return new URL(origin).host;
  } catch {
    return origin;
  }
}

/**
 * Opens the links the packaged app was opened with (docs/app-platform.md
 * 17): on the active server it goes there, the settings or any window over
 * the page closing as for a link pressed in the app (closeWindowsForLink, so
 * a migration's report handed over during an import shows; a settings address
 * opens the settings over the page as it is), on another server this app knows
 * it switches to it; a server it doesn't know yet is opened only once the
 * person says so, so a link alone can't put a strange server's screens in
 * the app. It shows in the signed-in app and on the sign-in screen
 * (signedOut), where a link to that server keeps its place for after signing
 * in or opens as the screen's own step — one at a time, as the first start takes the links itself, so
 * no two take one link; links that come in between wait in the app.
 */
export function PackagedLinkOpener({ signedOut = false }: { signedOut?: boolean }) {
  const { t } = useTranslation("sidebar");
  const [asking, setAsking] = useState<PackagedLinkTarget | null>(null);

  useEffect(() => listenForPackagedLinks((links) => {
    // A server is known at its ID and where it is reached now.
    const known = listProfiles().flatMap((profile) => [profile.id, reachedAt(profile)]);
    const link = links[links.length - 1];
    const target = packagedLinkTarget(link, serverOrigin(), known);
    if (!target) return;
    // A phone browser's "open in the app" asks the app's sign-in through the
    // browser where the app isn't signed in on that server (AuthGate): this
    // sign-in screen's own, or another server's.
    const server = target.server || serverIdentity();
    if (linkAsksBrowserSignIn(link) && (signedOut || server !== serverIdentity())) askBrowserSignIn(server);
    // An address of the active profile's own, its pair's other server serving it now.
    if (!target.server || target.server === serverIdentity()) {
      setAsking(null);
      if (signedOut && signInStepPath(target.path)) openSignInStep(target.path);
      else if (signedOut) returnAfterSignInTo(target.path);
      else pushRoute(target.path);
    } else if (known.includes(target.server)) {
      openProfile(profileAt(target.server)?.id ?? target.server, "", target.path);
    } else {
      setAsking(target);
    }
  }), [signedOut]);

  if (!asking) return null;
  const host = hostOf(asking.server);
  const open = () => {
    const profile = addServerProfile(asking.server);
    if (profile) openProfile(profile.id, "", asking.path);
    setAsking(null);
  };
  return createPortal((
    <ConfirmCard message={t("openLink.title", { host })} detail={t("openLink.body", { host })}
      confirmLabel={t("openLink.open")} cancelLabel={t("openLink.cancel")}
      markers={{ dialog: { "data-packaged-link": asking.server }, confirm: { "data-packaged-link-open": "" } }}
      onAnswer={(choice) => (choice === null ? setAsking(null) : open())} />
  ), document.body);
}
