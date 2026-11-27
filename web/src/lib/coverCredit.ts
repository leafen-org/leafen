export const UNSPLASH_REFERRAL_URL = "https://unsplash.com/?utm_source=leafen&utm_medium=referral";

/** Public credit travels with its picture as a versioned URL fragment (the
 * server's selectPhoto writes it on a photo chosen from Unsplash). The
 * fragment never reaches the CDN and needs no remote lookup to read it. */
export function coverCredit(value?: string): { author: string; profileUrl: string } | null {
  if (!value || value.length > 8192) return null;
  try {
    const image = new URL(value);
    if (image.protocol !== "https:" || image.host !== "images.unsplash.com" || image.username || image.password
      || !image.pathname.startsWith("/photo-")) return null;
    const credit = new URLSearchParams(image.hash.slice(1));
    if (credit.get("leafen-unsplash") !== "1") return null;
    const author = credit.get("author")?.trim() ?? "";
    const profile = new URL(credit.get("profile") ?? "");
    if (!author || [...author].length > 120 || /[\u0000-\u001f\u007f]/u.test(author)
      || profile.protocol !== "https:" || profile.host !== "unsplash.com" || profile.username || profile.password
      || !/^\/@[A-Za-z0-9_-]+$/u.test(profile.pathname)) return null;
    profile.hash = "";
    profile.search = "?utm_source=leafen&utm_medium=referral";
    return { author, profileUrl: profile.toString() };
  } catch { return null; }
}
