import type { LegalLinks } from "../runtimeConfigWire.generated";

export type { LegalLinks } from "../runtimeConfigWire.generated";

const UPSTREAM_REPOSITORY_URL = "https://github.com/leafen-org/leafen";

/**
 * A page of Leafen's own documentation (`docs/<path>` of its repository, with
 * a section's anchor after "#"): the one address every help link of the
 * screens takes, never another product's help center.
 */
export function leafenDocsUrl(path: string): string {
  return `${UPSTREAM_REPOSITORY_URL}/blob/main/docs/${path}`;
}

/** The license notices of the software Leafen is distributed with, served with its screens. */
export const THIRD_PARTY_NOTICES_PATH = "/THIRD-PARTY-NOTICES.txt";

export const DEFAULT_LEGAL_LINKS: LegalLinks = Object.freeze({
  sourceUrl: UPSTREAM_REPOSITORY_URL,
});
