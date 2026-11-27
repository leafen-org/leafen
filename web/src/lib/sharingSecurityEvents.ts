// An administrator changed the organization's sharing and security policy in
// the settings window over a page: what the page behind already drew from it
// reads it again (2026-11-06: user HTML switched off went on running in the
// open page until a reload, so the switch seemed not to work).

const SHARING_SECURITY_CHANGED_EVENT = "leafen:sharing-security-changed";

/** What a page's blocks read from the policy. */
export type SharingSecurityChange = { userHtml: boolean };

export function announceSharingSecurityChanged(change: SharingSecurityChange) {
  if (typeof window === "undefined") return;
  window.dispatchEvent(new CustomEvent<SharingSecurityChange>(SHARING_SECURITY_CHANGED_EVENT, { detail: change }));
}

export function onSharingSecurityChanged(listener: (change: SharingSecurityChange) => void): () => void {
  if (typeof window === "undefined") return () => {};
  const heard = (event: Event) => listener((event as CustomEvent<SharingSecurityChange>).detail);
  window.addEventListener(SHARING_SECURITY_CHANGED_EVENT, heard);
  return () => window.removeEventListener(SHARING_SECURITY_CHANGED_EVENT, heard);
}
