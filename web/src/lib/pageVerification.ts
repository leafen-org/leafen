import type { Page } from "./types";

export function isPageVerified(page?: Pick<Page, "verifiedAt" | "verificationExpiresAt"> | null, now = Date.now()) {
  if (!page?.verifiedAt) return false;
  if (!page.verificationExpiresAt) return true;
  const expiresAt = new Date(page.verificationExpiresAt).getTime();
  return Number.isNaN(expiresAt) || expiresAt > now;
}
