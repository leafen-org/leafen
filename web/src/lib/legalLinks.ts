import type { LegalLinks } from "../runtimeConfigWire.generated";

export type { LegalLinks } from "../runtimeConfigWire.generated";

const UPSTREAM_REPOSITORY_URL = "https://github.com/leafen-org/leafen";

/** The license notices of the software Leafen is distributed with, served with its screens. */
export const THIRD_PARTY_NOTICES_PATH = "/THIRD-PARTY-NOTICES.txt";

export const DEFAULT_LEGAL_LINKS: LegalLinks = Object.freeze({
  sourceUrl: UPSTREAM_REPOSITORY_URL,
});
