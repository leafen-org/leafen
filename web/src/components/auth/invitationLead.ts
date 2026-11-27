/**
 * asksToJoin: a teamspace's link, which lets someone new ask to join rather than join.
 * pageId: the page a guest invite link opens on once its person is in (useSignupInvitation.ts).
 */
export type InvitationDetails = { inviterName: string; workspaceName: string; pageTitle: string; message: string; asksToJoin?: boolean; pageId?: string };

/**
 * Who invited and to what, as far as it knows: the lead's first sentence under
 * the short heading, so a long place's name wraps in the lead, not in the
 * middle of the heading's words (2026-11-06).
 */
export function invitationLead(details: InvitationDetails, t: (key: string, options?: Record<string, string>) => string) {
  const page = details.pageTitle.trim();
  const place = page || details.workspaceName;
  // A place named after who invited says it once.
  if (details.inviterName && place && !place.includes(details.inviterName)) return t(page ? "authGate:invitationLeadPageBy" : "authGate:invitationLeadBy", { inviter: details.inviterName, place });
  return place ? t("authGate:invitationLeadTo", { place }) : "";
}
