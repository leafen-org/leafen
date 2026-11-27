/** The services the publish tab's "소셜 미디어로 공유" hands the site's link
 * to, in its order, as Notion's (seen 2026-11-11): X, WhatsApp, LinkedIn,
 * Facebook and an email. */
export const SOCIAL_SHARE_SERVICES = [
  { id: "x", name: "X" },
  { id: "whatsapp", name: "WhatsApp" },
  { id: "linkedin", name: "LinkedIn" },
  { id: "facebook", name: "Facebook" },
  { id: "email", name: "Email" },
] as const;

/** The round buttons under a published site's "사이트 공유" panel, as Notion's
 * site shows its visitors (seen 2026-11-06): LinkedIn, X, WhatsApp, Facebook
 * and an email. */
export const SITE_VISITOR_SHARE_SERVICES = ["linkedin", "x", "whatsapp", "facebook", "email"] as const;

export type SocialShareService = typeof SOCIAL_SHARE_SERVICES[number]["id"];

/** Where each service takes a link to share it. */
export function socialShareHref(service: SocialShareService, link: string, title: string) {
  const url = encodeURIComponent(link);
  if (service === "x") return `https://x.com/intent/post?url=${url}&text=${encodeURIComponent(title)}`;
  if (service === "linkedin") return `https://www.linkedin.com/sharing/share-offsite/?url=${url}`;
  if (service === "whatsapp") return `https://wa.me/?text=${encodeURIComponent(title ? `${title} ${link}` : link)}`;
  if (service === "email") return `mailto:?subject=${encodeURIComponent(title)}&body=${url}`;
  return `https://www.facebook.com/sharer/sharer.php?u=${url}`;
}

/**
 * The code that puts a published page in another site, as Notion's "이 페이지
 * 임베드" gives it (its help, read 2026-10-12): a frame of the page alone,
 * with or without its title.
 */
export function siteEmbedCode(siteLink: string, showTitle: boolean) {
  let src = siteLink;
  try {
    const url = new URL(siteLink);
    url.searchParams.set("embed", "1");
    if (!showTitle) url.searchParams.set("title", "0");
    src = url.toString();
  } catch {
    // An address that isn't a URL stays as it is.
  }
  const escaped = src.replace(/&/gu, "&amp;").replace(/"/gu, "&quot;");
  return `<iframe src="${escaped}" width="100%" height="600" frameborder="0" allowfullscreen></iframe>`;
}
