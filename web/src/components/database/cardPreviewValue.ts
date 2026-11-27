import { isBundledGalleryCover } from "@/lib/covers";
import type { DbProperty, Page } from "@/lib/types";
import { managedFileValueLocator, nativeWorkspaceFileIdFromLocator } from "@/lib/workspaceFileTransport";
import { fileAttachmentAddress, firstImageAttachment } from "./files";
import {
  CARD_PREVIEW_NONE,
  CARD_PREVIEW_PAGE,
  CARD_PREVIEW_PAGE_CONTENT,
  cardPreviewMode,
} from "./cardPreviewModePolicy";

function firstString(value: unknown): string {
  if (Array.isArray(value)) return firstString(value[0]);
  if (typeof value === "string") return value.trim();
  return "";
}

function isDisplayableCover(value: string) {
  return (
    isBundledGalleryCover(value) ||
    value.startsWith("linear-gradient") ||
    value.startsWith("data:image/") ||
    value.startsWith("blob:") ||
    /^https?:\/\//i.test(value) ||
    // An uploaded file, as its ID or its address.
    Boolean(nativeWorkspaceFileIdFromLocator(managedFileValueLocator(value)))
  );
}

export function cardCoverValue(row: Page, props: DbProperty[], coverProperty?: string) {
  const mode = cardPreviewMode(coverProperty);
  if (mode === CARD_PREVIEW_NONE) return "";
  if (mode === CARD_PREVIEW_PAGE_CONTENT) return "";
  if (mode === CARD_PREVIEW_PAGE) return isDisplayableCover(row.cover ?? "") ? (row.cover ?? "") : "";
  const prop = props.find((item) => item.id === mode);
  if (!prop) return "";
  if (prop.type === "files") {
    const rawFiles = row.properties?.[prop.id];
    const file = firstImageAttachment(rawFiles);
    const address = file ? fileAttachmentAddress(file) : "";
    return isDisplayableCover(address) ? address : "";
  }
  const value = firstString(row.properties?.[prop.id]);
  return isDisplayableCover(value) ? value : "";
}

export function coverBackground(value: string) {
  if (!value) return undefined;
  if (value.startsWith("linear-gradient")) return value;
  return `url("${value.replace(/"/g, '\\"')}")`;
}
