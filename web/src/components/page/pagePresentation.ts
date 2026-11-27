import { safeStoredFileUrl } from "@/lib/urls";
import type { Page } from "@/types";
import { coverGradientImage } from "./coverGradient";

export type PagePresentationChange = Partial<Pick<
  Page,
  "icon" | "iconType" | "cover" | "coverPosition" | "font" | "smallText" | "fullWidth" | "backlinksDisplay" | "pageCommentsDisplay" | "tableOfContentsDisplay" | "inlineCommentsDisplay"
>>;

const unsetPresentation: Required<PagePresentationChange> = {
  icon: "", iconType: "none", cover: "", coverPosition: 50, font: "default", smallText: false, fullWidth: false,
  backlinksDisplay: "default", pageCommentsDisplay: "default", tableOfContentsDisplay: "default", inlineCommentsDisplay: "default",
};

/** What `change` replaces on a page or row page, to put back. */
export function presentationBefore(shown: PagePresentationChange, change: PagePresentationChange) {
  const before: Record<string, unknown> = {};
  for (const key of Object.keys(change) as (keyof PagePresentationChange)[]) before[key] = shown[key] ?? unsetPresentation[key];
  return before as PagePresentationChange;
}

export function safePresentationImage(value?: string) {
  return safeStoredFileUrl(value, ["data:image/"]);
}

export function safeExternalPresentationImage(value?: string) {
  const candidate = safePresentationImage(value);
  try {
    const url = new URL(candidate);
    return url.protocol === "https:" || url.protocol === "http:" ? candidate : "";
  } catch {
    return "";
  }
}

export function coverImage(cover: string, resolvedUrl?: string) {
  if (cover.startsWith("linear-gradient")) return coverGradientImage(cover) ?? cover;
  const url = resolvedUrl ?? safePresentationImage(cover);
  return url ? `url("${url.replace(/"/gu, '\\"')}")` : "none";
}
