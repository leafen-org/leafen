import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "@/api";
import { useWaitingNote } from "@/lib/pendingChoice";
import { serverPath } from "@/lib/serverOrigin";
import { uploadWorkspaceFile } from "@/lib/storage";
import type { Session } from "@/types";
import { uploadRefusalReason } from "@/lib/uploadRefusal";

export type SitePreviewSettings = {
  previewImageFileId: string;
  faviconFileId: string;
  googleAnalyticsId: string;
  /** Whether search engines may index the site ("검색 엔진 인덱싱"); off at first. */
  allowIndexing: boolean;
  /** Whether visitors may duplicate its pages ("템플릿으로 복제"); on at first, as Notion's. */
  allowDuplicate: boolean;
  /** Whether the site's header shows its share button, as Notion's "공유" switch. */
  showShare: boolean;
};

export const SITE_IMAGE_TYPES = "image/png,image/jpeg,image/gif,image/webp";
// A measurement ID (G-) or a Google tag ID (GT-), as Notion takes either (seen 2026-11-06).
const MEASUREMENT_ID = /^GT?-[A-Z0-9]{4,20}$/u;

export function validGoogleAnalyticsId(value: string) {
  return value === "" || MEASUREMENT_ID.test(value);
}

/** An image the site keeps (its share preview or favicon), as a workspace file. */
export function SiteImageThumbnail({ session, fileId, className }: { session: Session; fileId: string; className?: string }) {
  const [url, setURL] = useState("");
  useEffect(() => {
    let current = true;
    setURL("");
    void api.managedFileCapability(session, fileId).then((capability) => {
      if (current) setURL(serverPath(capability.url));
    }).catch(() => undefined);
    return () => { current = false; };
  }, [fileId, session]);
  return url ? <img className={className} src={url} alt="" /> : null;
}

/**
 * Uploads the published site's share preview image or favicon into the
 * workspace; the site shows them to anyone only while it is published.
 */
export function useSiteImageUpload(workspaceId: string, pageId: string,
  value: SitePreviewSettings, onChange: (next: SitePreviewSettings) => void) {
  const { t } = useTranslation(["siteBuilder"]);
  const [uploading, setUploading] = useState<"" | "preview" | "favicon">("");
  const [error, setError] = useState("");
  // Ten seconds without the server's answer, the editor says so (lib/storage.ts waits).
  const waitingNote = useWaitingNote(uploading !== "");
  async function upload(kind: "preview" | "favicon", file?: File) {
    if (!file || !file.type.startsWith("image/")) return;
    setUploading(kind);
    setError("");
    try {
      const uploaded = await uploadWorkspaceFile(file, "uploads", { workspaceId, pageId });
      onChange(kind === "preview"
        ? { ...value, previewImageFileId: uploaded.fileId }
        : { ...value, faviconFileId: uploaded.fileId });
    } catch (caught) {
      setError(uploadRefusalReason(caught) ?? t("siteBuilder:assetUploadFailed"));
    } finally {
      setUploading("");
    }
  }
  return { uploading, error, waitingNote, upload };
}
