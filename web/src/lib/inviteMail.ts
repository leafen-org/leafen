import { addressReach } from "./addressReach";
import { hasPublishedAddress, linkOrigin } from "./serverOrigin";

/**
 * What became of the mail an invitation by address asks for, said alike by
 * every place that invites one — the share window's notice and the invitee's
 * row, and "멤버 추가하기": sent with its link; sent without one, as a server
 * with no public address can't make links others open, so the link is the
 * inviter's to hand on; not sent at all, where the server sends no mail;
 * held back by the limits on invitation mail per inviter and per address
 * (page_share_invite_emails.go), where it does; or failed, every try or the
 * last one. Notion always mails, so only the first is Notion's state.
 */
export type InviteMail = "sent" | "sentWithoutLink" | "notSent" | "limited" | "failed";

/**
 * The mail's fate, from whether the server sends mail (it has read its
 * runtime config) and what the server kept of this invitation's mail, when
 * it didn't go.
 */
export function inviteMail(mailSends: boolean, held: "limited" | "failed" | "" = ""): InviteMail {
  if (!mailSends) return "notSent";
  if (held) return held;
  // Not with a link at an address of this computer alone, which the server
  // doesn't send (its othersLinkOrigin).
  return hasPublishedAddress() && addressReach(linkOrigin()) !== "loopback" ? "sent" : "sentWithoutLink";
}
