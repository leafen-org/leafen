import { useEffect, useRef, useState } from "react";
import { PACKAGED_ORIGINS } from "./packagedOrigins";
import { serverPath } from "./serverOrigin";

/**
 * The site editor's preview, as Notion's (seen 2026-11-05): the published
 * site itself in a frame, its header and theme following the settings being
 * edited before they are published. The editor sends the draft into the
 * frame; the frame says when its header is picked. The draft only changes
 * how the frame draws the site, never what it holds.
 */
export type SitePreviewDraft = {
  title?: string;
  theme: "system" | "light" | "dark";
  showBreadcrumbs: boolean;
  showSearch: boolean;
  showBranding: boolean;
  allowDuplicate: boolean;
  showShare: boolean;
  navigationPageIds: string[];
  /** The part the editor has open, outlined as Notion's. */
  picked: "" | "header";
};

const DRAFT = "leafen-site-preview";
const READY = "leafen-site-preview-ready";
const PICK = "leafen-site-preview-pick";

/**
 * The site's address in the editor's frame: its path on the server the
 * editor itself talks to, not the server's set address, so the frame is the
 * editor's own origin however the server was reached (2026-11-13: opened as
 * a.localhost, the frame at the set 127.0.0.1 was refused as another site's).
 */
export function sitePreviewSrc(siteLink: string) {
  let path = siteLink;
  try {
    const url = new URL(siteLink);
    path = `${url.pathname}${url.search}`;
  } catch {
    // Already a path.
  }
  const address = serverPath(path);
  return `${address}${address.includes("?") ? "&" : "?"}preview=1`;
}

/** The editor's side: sends the draft as it changes and hears the header picked. */
export function useSitePreviewFrame(frame: HTMLIFrameElement | null, draft: SitePreviewDraft, onPick: (part: "header") => void) {
  const [ready, setReady] = useState(0);
  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      if (!frame || event.source !== frame.contentWindow) return;
      const data = event.data as { type?: string; part?: string } | null;
      if (data?.type === READY) setReady((value) => value + 1);
      if (data?.type === PICK && data.part === "header") onPick("header");
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [frame, onPick]);
  const json = JSON.stringify(draft);
  useEffect(() => {
    if (!frame?.contentWindow || !frame.src) return;
    frame.contentWindow.postMessage({ type: DRAFT, draft: JSON.parse(json) as SitePreviewDraft }, new URL(frame.src).origin);
  }, [frame, json, ready]);
}

/**
 * The site's side: in the editor's frame, the draft it is sent, and a way to
 * say its header was picked. Only the editor may send a draft: the server's
 * own page (the site's origin) or the packaged client's. An embed of the site
 * ("이 페이지 임베드") sits in anyone's frame, so it never previews.
 */
export function useSitePreviewDraft(previewing: boolean) {
  const [draft, setDraft] = useState<SitePreviewDraft | null>(null);
  const editorOrigin = useRef("");
  useEffect(() => {
    if (!previewing || window.parent === window) return undefined;
    const onMessage = (event: MessageEvent) => {
      if (event.source !== window.parent || !isEditorOrigin(event.origin)) return;
      const data = event.data as { type?: string; draft?: SitePreviewDraft } | null;
      if (data?.type !== DRAFT || !data.draft) return;
      editorOrigin.current = event.origin;
      setDraft(data.draft);
    };
    window.addEventListener("message", onMessage);
    window.parent.postMessage({ type: READY }, "*");
    return () => window.removeEventListener("message", onMessage);
  }, [previewing]);
  const pick = () => {
    if (editorOrigin.current) window.parent.postMessage({ type: PICK, part: "header" }, editorOrigin.current);
  };
  return { draft, pick };
}

function isEditorOrigin(origin: string) {
  return origin === window.location.origin || PACKAGED_ORIGINS.includes(origin);
}
