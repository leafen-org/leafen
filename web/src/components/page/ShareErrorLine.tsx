import type { ReactNode } from "react";
import type { useTranslation } from "react-i18next";
import { LeafenApiError } from "@/apiTransport";
import { accessRequestErrorCodes } from "@/accessRequestWire.generated";
import { userErrorText, writeUncertainMark } from "@/lib/settingsErrors";
import { organizationErrorCodes } from "@/organizationWire.generated";
import { publicMutationActions, publicMutationBounds, publicMutationErrorCodes, publicMutationFields } from "@/publicMutationWire.generated";
import styles from "./ShareErrorLine.module.css";

/** What a change in the share window was: a site's (the "게시" tab's) or its sharing's, and whether it was the site's address. */
export type ShareChangeKind = { action?: string; address?: boolean };

/** The site's fields the share window says a refusal under: its address, Analytics ID, images, link title and description. */
const SITE_FIELDS = [publicMutationFields.slug, publicMutationFields.googleAnalyticsId, publicMutationFields.previewImageFileId,
  publicMutationFields.faviconFileId, publicMutationFields.title, publicMutationFields.description] as const;
export type ShareSiteField = typeof SITE_FIELDS[number];
/** Why a refused change can't be what the site's fields hold, by field (shareFieldErrors). */
export type ShareFieldErrors = Partial<Record<ShareSiteField, string>>;

/**
 * Why a refused change can't be what the site's fields hold, each said under
 * its own field (ShareErrorLine): an address another site has, or the fields
 * the server's refusal names (its error fields, public_mutation_refusals.go)
 * — an address, an Analytics ID or an image the site can't have, a link
 * title or description over its length. A field the window has no place
 * for goes with the window's own words at the end (shareErrorText).
 */
export function shareFieldErrors(error: unknown, t: ReturnType<typeof useTranslation>["t"]): ShareFieldErrors {
  if (!(error instanceof LeafenApiError)) return {};
  if (error.code === publicMutationErrorCodes.siteAddressTaken) return { slug: t("siteBuilder:addressTaken") };
  if (error.code !== publicMutationErrorCodes.invalid) return {};
  const most = publicMutationBounds.textRunes.toLocaleString();
  const words: Record<ShareSiteField, () => string> = {
    slug: () => t("siteBuilder:addressInvalid"),
    googleAnalyticsId: () => t("siteBuilder:googleAnalyticsInvalid"),
    previewImageFileId: () => t("siteBuilder:previewImageInvalid"),
    faviconFileId: () => t("siteBuilder:faviconInvalid"),
    title: () => t("siteBuilder:linkTitleTooLong", { most }),
    description: () => t("siteBuilder:descriptionTooLong", { most }),
  };
  const said: ShareFieldErrors = {};
  for (const field of SITE_FIELDS) if (error.fields?.includes(field)) said[field] = words[field]();
  return said;
}

/**
 * Why a change in the share window didn't take, in the reader's words, by
 * the server's refusal: the organization's or teamspace's sharing security,
 * a guest only owners invite, another site's address, an address the site
 * can't have, the first site field a refusal names (shareFieldErrors), the
 * sharing changed elsewhere since it was read, the page
 * gone out of reach, or the page in the trash; else what any refusal says
 * (userErrorText — a page held for a while, the server busy or unanswered),
 * else that the site or the sharing couldn't be changed. A person's page,
 * which doesn't unlink, goes back with no word, as Notion's
 * (people_page_linked.go).
 */
export function shareErrorText(error: unknown, t: ReturnType<typeof useTranslation>["t"], change: ShareChangeKind = {}) {
  const site = change.action === publicMutationActions.publishSite || change.action === publicMutationActions.unpublishSite;
  const code = error instanceof LeafenApiError ? error.code : undefined;
  switch (code) {
    case organizationErrorCodes.sharingSecurityBlocked: return t("topBar:sharingSecurityBlocked");
    case accessRequestErrorCodes.guestInviteRequiresApproval: return t("topBar:guestInviteNeedsApproval");
    case publicMutationErrorCodes.siteAddressTaken: return t("siteBuilder:addressTaken");
    case publicMutationErrorCodes.personPageLinked: return "";
    case publicMutationErrorCodes.revisionConflict: return t("topBar:shareChangedElsewhere");
    case publicMutationErrorCodes.notFound: return t("topBar:shareOutOfReach");
    case "LEAFEN_PAGE_IN_TRASH": return t("productErrors:meanings.pageInTrashCantChange");
    case publicMutationErrorCodes.invalid: {
      if (change.address) return t("siteBuilder:addressInvalid");
      const named = Object.values(shareFieldErrors(error, t))[0];
      if (named) return named;
    }
  }
  return userErrorText(error, t(site ? "siteBuilder:changeFailed" : "topBar:shareFailed"));
}

/**
 * The line that says it, the one the share window draws wherever a change
 * is made — the share tab's end, the "게시" tab before its page is published,
 * its published panel (under the address row, or at the panel's end), its
 * search indexing screen and the site editor (under a field whose words the
 * site can't have, or at its panel's end) — red as every failure's
 * words (--danger), or, for a change whose outcome isn't known, the
 * secondary colour (writeUncertainMark); so too, said politely, a change
 * still waiting on the server's answer (waiting, useWaitingNote). The place
 * it stands in gives its margins (className).
 */
export function ShareErrorLine({ text, className, waiting, children, ...data }: {
  text: string; className?: string; waiting?: boolean; children?: ReactNode;
} & { [mark: `data-${string}`]: string | boolean | undefined }) {
  if (!text) return null;
  return <p className={className ? `${styles.line} ${className}` : styles.line} role={waiting ? "status" : "alert"}
    {...(waiting ? { "data-write-uncertain": "" } : writeUncertainMark(text))} {...data}>{text}{children}</p>;
}
