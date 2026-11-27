import { type Dispatch, type SetStateAction, useState } from "react";
import { authErrorCodes } from "@/authWire.generated";
import { authErrorLine, type AuthText } from "@/lib/authErrorPresentation";
import { pageHref } from "@/lib/navigation";
import { replaceRoute } from "@/lib/router";
import { dropSignupInvitationFromAddress, readSignupInvitation } from "@/lib/signupInvitation";
import type { AuthGateControllerStep } from "./authGateStart";
import type { InvitationDetails } from "./invitationLead";

const NO_DETAILS: InvitationDetails = { inviterName: "", workspaceName: "", pageTitle: "", message: "" };

export type SignupInvitationState = ReturnType<typeof useSignupInvitation>;

/**
 * The invitation the sign-in page's address carries (lib/signupInvitation.ts)
 * — an email's, a name's, a workspace's or teamspace's invite link, a guest
 * invite link — and what it says: the address it was sent to, who invited to
 * what. Where sign-up is closed only one makes an account.
 *
 * `end` is the one place an invitation the server says can't be used (used,
 * withdrawn, turned off, past its end) leaves the page, as the page opens or
 * as its name is sent: sign-in opens with why, and from then on making an
 * account is the server's own sign-up, as with no invitation (2026-11-23:
 * '계정 만들기' opened the ended invitation's name step again). A name the
 * page suggested goes with it; one the person gave stays. Any other failure
 * (the server not answering) keeps the invitation, to try again.
 */
export function useSignupInvitation({ setStep, setError, setDisplayName }: {
  setStep: Dispatch<SetStateAction<AuthGateControllerStep>>;
  setError: Dispatch<SetStateAction<AuthText>>;
  setDisplayName: Dispatch<SetStateAction<string>>;
}) {
  const [token, setToken] = useState(readSignupInvitation);
  const [email, setEmail] = useState("");
  const [details, setDetails] = useState<InvitationDetails>(NO_DETAILS);
  return {
    token, email, details, setEmail, setDetails,
    /**
     * Its account made, the invitation leaves the address. A guest invite
     * link opens on its first page left, as its first screen named it: its
     * own address names its first page, which may be in the trash now
     * (2026-11-23: the person landed on "페이지 찾지 못함").
     */
    leaveAddress() {
      if (details.pageId) replaceRoute(pageHref(details.pageId));
      else dropSignupInvitationFromAddress();
    },
    /** Whether `caught` ended the invitation, which then left the page as said above. */
    end(caught: unknown, { suggestedName = false } = {}): boolean {
      if (!token || (caught as { code?: unknown } | null)?.code !== authErrorCodes.signupInvitationInvalid) return false;
      setToken("");
      setEmail("");
      setDetails(NO_DETAILS);
      if (suggestedName) setDisplayName("");
      setStep("signin");
      setError(authErrorLine(caught, "auth.signUp"));
      return true;
    },
  };
}
