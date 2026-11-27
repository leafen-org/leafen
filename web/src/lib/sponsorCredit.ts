import type { SponsorCredit } from "@/runtimeConfigWire.generated";

/**
 * The server's sponsor list as the runtime settings carry it, each sponsor
 * with its share — kept for the home's sponsors row to come after release
 * (user decision 2026-11-25), which no screen shows yet. Anything malformed
 * is dropped. The list is empty only where LEAFEN_SPONSOR_CREDIT=off, which
 * also takes away the workspace menu's GitHub line (sponsorThanksShown).
 */
export function normalizeSponsorCredits(value: unknown): SponsorCredit[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((raw): SponsorCredit[] => {
    const record = raw && typeof raw === "object" ? raw as Record<string, unknown> : {};
    const kind = record.kind === "house" || record.kind === "sponsor" ? record.kind : null;
    const url = httpsUrl(record.url);
    const name = typeof record.name === "string" ? record.name.trim() : "";
    const logo = typeof record.logo === "string" && /^data:image\/(?:png|webp);base64,[A-Za-z0-9+/=]+$/u.test(record.logo)
      ? record.logo
      : "";
    const weight = typeof record.weight === "number" && Number.isFinite(record.weight) && record.weight > 0
      ? record.weight
      : 0;
    if (!kind || !url || weight === 0 || (kind === "sponsor" && (!name || !logo))) return [];
    const slogan = kind === "sponsor" ? sloganOf(record.slogan) : undefined;
    return [{ kind, name, url, logo, weight, ...(slogan ? { slogan } : {}) }];
  });
}

// A sponsor's one-line slogan by language; anything else is dropped.
function sloganOf(value: unknown): Record<string, string> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const slogan: Record<string, string> = {};
  for (const [language, text] of Object.entries(value as Record<string, unknown>).slice(0, 80)) {
    if (/^[a-z]{2,3}(?:-[A-Za-z0-9]{2,8})?$/u.test(language) && typeof text === "string" && text.trim() && text.length <= 200) {
      slogan[language] = text.trim();
    }
  }
  return Object.keys(slogan).length ? slogan : undefined;
}

function httpsUrl(value: unknown): string | null {
  if (typeof value !== "string") return null;
  try {
    const url = new URL(value);
    return url.protocol === "https:" && !url.username && !url.password ? url.toString() : null;
  } catch {
    return null;
  }
}

/** Whether the server shows Leafen's thanks: off only with LEAFEN_SPONSOR_CREDIT=off (sponsor_credit.go). */
export function sponsorThanksShown(credits: readonly SponsorCredit[]): boolean {
  return credits.length > 0;
}
