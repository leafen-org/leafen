import { useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/api";
import { isManagedFileBlock, type GraphBlock } from "@/contentGraph";
import type { Session } from "@/types";
import { triggerBrowserLinkDownload, triggerBrowserURLDownload, withFileDownloadName } from "@/lib/browserFileDownload";
import { createFileDownloadCapabilityResolver } from "@/lib/fileDownloadCapability";
import { usePublicFileUrl } from "@/lib/publicFileUrls";
import { enterpriseDLPErrorCodes } from "@/enterpriseDLPWire.generated";
import { safeMediaUrl } from "./mediaBlockUrl";
import { canPreviewPassively, createPassiveFilePreviewObjectURL } from "@/lib/passiveFilePreview";

/** File commands resolve at the current file authority, including published
 * readers. A response for a replaced file or closed reader cannot act. */
export function useBlockFileActions(block: GraphBlock, session: Session | null) {
  const publicFileUrl = usePublicFileUrl();
  const fileId = isManagedFileBlock(block) ? String(block.content.fileId ?? "") : "";
  const external = fileId ? "" : safeMediaUrl(block.content.url);
  const name = String(block.content.name ?? block.content.fileName ?? "");
  const contentType = String(block.content.contentType ?? "");
  const source = useMemo(() => ({ fileId, external, session, publicFileUrl,
    resolver: fileId && session && !publicFileUrl
      ? createFileDownloadCapabilityResolver(() => api.managedFileCapability(session, fileId)) : null,
  }), [fileId, external, session, publicFileUrl]);
  const current = useRef<typeof source | null>(source); current.current = source;
  const [error, setError] = useState(false);
  // The organization's data loss prevention keeps the file in: said as that,
  // not as a file that failed to load (2026-11-06).
  const [blocked, setBlocked] = useState(false);
  const fail = (cause: unknown) => {
    if (current.current !== source) return;
    setError(true);
    setBlocked((cause as { code?: unknown } | null)?.code === enterpriseDLPErrorCodes.policyBlocked);
  };
  useEffect(() => {
    current.current = source; setError(false); setBlocked(false);
    return () => { source.resolver?.clear(); if (current.current === source) current.current = null; };
  }, [source]);
  async function resolve() {
    const url = source.fileId ? source.publicFileUrl ? source.publicFileUrl(source.fileId)
      : source.resolver ? (await source.resolver.resolve()).url : "" : source.external;
    return current.current === source ? url : "";
  }
  // Each try starts clear: an earlier refusal doesn't outlive a try that works.
  function clear() { setError(false); setBlocked(false); }
  async function download() {
    clear();
    try {
      const url = await resolve();
      if (!url) return;
      const saved = source.resolver ? await triggerBrowserURLDownload(url, name)
        : await triggerBrowserLinkDownload(source.fileId ? withFileDownloadName(url, name) : url, name);
      if (saved === "failed") fail(new Error("The file couldn't be saved."));
    } catch (cause) { fail(cause); }
  }
  function open() {
    clear();
    // A file no tab can show is a download; a tab reserved for it stays blank.
    if (fileId && !canPreviewPassively(contentType)) { void download(); return; }
    // Reserve the tab during the click, before an asynchronous capability
    // read loses browser activation. Untrusted bytes never see an opener.
    const tab = window.open("about:blank", "_blank");
    if (!tab) { fail(null); return; }
    tab.opener = null;
    const referrer = tab.document.createElement("meta");
    referrer.name = "referrer"; referrer.content = "no-referrer"; tab.document.head.append(referrer);
    void resolve().then(async url => {
      if (!url) { tab.close(); return; }
      if (source.fileId) {
        const preview = await createPassiveFilePreviewObjectURL(url, contentType);
        if (current.current !== source || tab.closed) { if (preview) URL.revokeObjectURL(preview); tab.close(); return; }
        if (preview) {
          window.setTimeout(() => URL.revokeObjectURL(preview), 5 * 60 * 1000);
          tab.location.replace(preview); return;
        }
      }
      const link = tab.document.createElement("a"); link.href = url; link.rel = "noreferrer noopener";
      tab.document.body.append(link); link.click(); link.remove();
    }).catch((cause) => { tab.close(); fail(cause); });
  }
  return { available: Boolean(external || fileId && (session || publicFileUrl)), error, blocked, resolve,
    /** Opening this file downloads it, as no tab can show it. */
    downloadsOnOpen: Boolean(fileId && !canPreviewPassively(contentType)),
    isCurrent: () => current.current === source,
    open, download: () => { void download(); } };
}
