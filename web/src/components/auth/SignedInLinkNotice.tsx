import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { ConfirmCard } from "@/components/ConfirmActionDialog";
import { copyText } from "@/lib/copyText";
import { useProductToast } from "@/lib/productToast";
import { serverUrl } from "@/lib/serverOrigin";
import { setSignedInLinkNoticeShown } from "@/lib/signedInLinkNoticeShown";
import { pageHref } from "@/lib/navigation";
import { dropSignupInvitationFromAddress, guestLinkHeldText, invitationEndedText } from "@/lib/signupInvitation";
import { thisWindowKey } from "@/lib/thisWindowWords";
import { useAfterTyping } from "@/lib/typingHold";
import type { Session } from "@/types";
import type { LinkLeftWhileSignedIn } from "./initialAuthEntry";
import { takeSignupInvitation } from "./takeSignupInvitation";

/**
 * Someone else's invitation or device link opened in a browser signed in to
 * an account (initialAuthEntry.ts) — an invitation for an address the account
 * holds is taken instead (takeSignupInvitation.ts): it isn't used here, and the app says
 * whose link it is, which account this browser holds, and where to open it —
 * the invited person's device, or another browser — with the link to copy.
 * A browser holds one account a server, so it offers no second one here.
 * A guest invite link is any signed-in account's to take, so it shows here
 * only when it couldn't be taken: a guest link's card, saying why where the
 * server says, with nothing to copy (2026-11-23: it had the card of an
 * invitation that makes a new account). In Leafen's own app the card says
 * the app stays signed in, not "this browser" (2026-11-23).
 */
export function SignedInLinkNotice({ session, link, onClose }: {
  session: Session;
  link: LinkLeftWhileSignedIn;
  onClose(): void;
}) {
  const { t } = useTranslation("authGate");
  const { notify } = useProductToast();
  // Whom an invitation is for, "" for none named, null when it can't be used.
  const [invited, setInvited] = useState<string | null | undefined>(link.kind === "invitation" ? undefined : "");
  // Why it can't be used, where the server says (a guest invite link used, ended or turned off).
  const [ended, setEnded] = useState("");
  useEffect(() => {
    setSignedInLinkNoticeShown(true);
    return () => setSignedInLinkNoticeShown(false);
  }, []);

  useEffect(() => {
    if (link.kind !== "invitation") return undefined;
    dropSignupInvitationFromAddress();
    let current = true;
    // One for an address this account holds is its own: taken, the app opens
    // again where it leads, with what it let the account into.
    // A guest invite link one of whose pages is held a while isn't taken, and says so (lib/signupInvitation.ts).
    let held = "";
    void takeSignupInvitation(session, link.token, (caught) => { held = guestLinkHeldText(caught, t); }).then((taken) => {
      if (!current) return;
      if (taken) {
        window.location.assign(taken.pageId ? pageHref(taken.pageId) : `${window.location.pathname}${window.location.search}`);
        return;
      }
      if (held) {
        setEnded(held);
        setInvited(null);
        return;
      }
      void api.signupInvitation(link.token).then(
        (invitation) => { if (current) setInvited(invitation.name || invitation.email || ""); },
        (caught: unknown) => { if (current) { setEnded(invitationEndedText(caught, t)); setInvited(null); } },
      );
    });
    return () => { current = false; };
  }, [link, session, t]);

  const clear = useAfterTyping(invited !== undefined);
  if (invited === undefined || !clear) return null;
  const account = session.user.name || session.user.email;
  const address = link.kind === "device"
    ? serverUrl(`/device/${encodeURIComponent(link.code)}`)
    : serverUrl(`/#invitation=${encodeURIComponent(link.token)}`);
  const guestLink = link.kind === "invitation" && link.token.startsWith("pgl_");
  // Only a link another account or device may still use is offered to copy.
  const closeOnly = invited === null || guestLink;
  const message = t(link.kind === "device" ? "signedInLink.deviceTitle" : guestLink ? "signedInLink.guestLinkTitle" : "signedInLink.invitationTitle");
  // What stays signed in is the window it shows in: this browser, or Leafen's own app (lib/thisWindowWords.ts).
  const ofWindow = (key: "deviceBody" | "stillSignedIn" | "invitationUnusable" | "invitationBodyFor" | "invitationBody", values: Record<string, string>) =>
    t(thisWindowKey(`signedInLink.${key}`, `signedInLink.${key}App`), values);
  const detail = link.kind === "device" ? ofWindow("deviceBody", { account })
    : invited === null ? ended ? `${ended} ${ofWindow("stillSignedIn", { account })}` : ofWindow("invitationUnusable", { account })
      : guestLink ? t("signedInLink.guestLinkNotTaken", { account })
        : invited ? ofWindow("invitationBodyFor", { account, invited }) : ofWindow("invitationBody", { account });
  return createPortal((
    <ConfirmCard message={message} detail={detail}
      confirmLabel={closeOnly ? t("signedInLink.close") : t("signedInLink.copy")}
      cancelLabel={closeOnly ? null : t("signedInLink.close")}
      markers={{ dialog: { "data-signed-in-link": link.kind } }}
      onAnswer={(answer) => {
        onClose();
        if (answer === null || closeOnly) return;
        void copyText(address).then((copied) => { if (copied) notify(t("signedInLink.copied")); });
      }} />
  ), document.body);
}
