import { useEffect, useRef, useState } from "react";
import { request } from "@/apiTransport";
import { appPlatformBounds, appPlatformErrorCodes, appPlatformOperations, type AppHTMLFrame } from "@/appPlatformWire.generated";
import type { GraphBlock } from "@/contentGraph";
import { DotsHorizontal, OpenInNew } from "@/icons/leafen";
import { isSandboxedHtmlFile } from "@/lib/fileSecurity";
import { usePublicFileUrl } from "@/lib/publicFileUrls";
import { serverPath } from "@/lib/serverOrigin";
import { blockItemText } from "./blockItemLabels";
import { blockToolMenuAnchor, requestEditorBlockCommand } from "./editorCommands";
import { blockUploadProgressLabel } from "./fileDrop";
import { useMediaBlockUpload } from "./mediaBlockUpload";
import { BlockEmptyPopover, BlockEmptyUploadPane, BlockEmptyRow } from "./BlockEmptyPopover";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./HtmlEmbedBlock.module.css";
import { fileBlockAlignment } from "./blockFileMenuModel";
import { onSharingSecurityChanged } from "@/lib/sharingSecurityEvents";

// User HTML (docs/app-platform.md 14). An empty "HTML" block is
// Notion's gray row; pressed, it opens a window to choose or drop an HTML
// file (Notion's AI generation stays outside "no AI in the editor").
// An uploaded HTML file is drawn under its name, marked as user HTML, in a
// frame with no origin that the server opens with a ticket: no bridge and
// none of Leafen's data, so its own scripts may run.

export function HtmlEmbedEmpty({ adapter, block }: { adapter: NativeEditorGraphAdapter; block: GraphBlock }) {
  const { progress, error, setError, upload } = useMediaBlockUpload(adapter, block, "file");

  function choose(file: File | undefined) {
    if (!file) return;
    if (!isSandboxedHtmlFile(file)) {
      setError(blockItemText("htmlEmbed.wrongType"));
      return;
    }
    upload(file);
  }

  if (adapter.readOnly) return <BlockEmptyRow name="html" icon={<OpenInNew size="regular" />} label={blockItemText("htmlEmbed.title")} />;
  // The empty media blocks' row and window (BlockEmptyPopover); made from the
  // "/" menu, it opens on its upload choice, as Notion's file blocks do.
  return (
    <BlockEmptyPopover blockId={block.id} name="html" icon={<OpenInNew size="regular" />} label={blockItemText("htmlEmbed.title")}
      width={540} tabs={[{ id: "upload", label: blockItemText("htmlEmbed.tab") }]} phoneTitle={blockItemText("htmlEmbed.phoneTitle")} phoneHalf
      rowHint={<>
        {progress ? <div className={styles.status} role="status" aria-live="polite">
          {blockUploadProgressLabel(progress)} · {progress.fileName}</div> : null}
        {error ? <div className={styles.error} role="alert">{error}</div> : null}
      </>}>
      {() => (
        <BlockEmptyUploadPane accept=".html,.htm,text/html" choose={blockItemText("htmlEmbed.upload")} phoneChoose={blockItemText("htmlEmbed.phoneChoose")} progress={progress}
          drop={{ title: blockItemText("htmlEmbed.upload"), hint: blockItemText("htmlEmbed.drop"), icon: "</>" }}
          onFile={choose} />
      )}
    </BlockEmptyPopover>
  );
}

type FrameState = { kind: "loading" } | { kind: "ready"; url: string } | { kind: "off" } | { kind: "failed" } | { kind: "signedOut" };

export function HtmlEmbedFrame({ adapter, block, nested = false }: { adapter: NativeEditorGraphAdapter; block: GraphBlock; nested?: boolean }) {
  const fileId = String(block.content.fileId ?? "");
  const name = String(block.content.name ?? "HTML");
  const publicFileUrl = usePublicFileUrl();
  const [state, setState] = useState<FrameState>({ kind: "loading" });
  // User HTML switched off or on in the settings window over this page takes
  // effect here at once, not at the next opening; another switch there
  // leaves a running frame as it is.
  const [policyGeneration, setPolicyGeneration] = useState(0);
  const shown = useRef(state.kind);
  shown.current = state.kind;
  useEffect(() => onSharingSecurityChanged(({ userHtml }) => {
    if (userHtml === (shown.current !== "off")) return;
    setPolicyGeneration((n) => n + 1);
  }), []);

  useEffect(() => {
    const session = adapter.session;
    if (!fileId || (!session && !publicFileUrl)) {
      setState({ kind: fileId ? "signedOut" : "failed" });
      return undefined;
    }
    let live = true;
    setState({ kind: "loading" });
    // A published page asks its own public route; a person, the file's.
    const issue = publicFileUrl
      ? fetch(`${publicFileUrl(fileId)}/html-frame`).then(async (response) => {
        const body = await response.json().catch(() => null) as (AppHTMLFrame & { error?: { code?: string } }) | null;
        if (!response.ok) throw new Error(body?.error?.code ?? "failed");
        return body as AppHTMLFrame;
      })
      : request<AppHTMLFrame>(appPlatformOperations.htmlFrame.path.replace("{id}", encodeURIComponent(fileId)),
        { method: appPlatformOperations.htmlFrame.method }, session);
    issue.then(
      (frame) => { if (live) setState({ kind: "ready", url: serverPath(frame.frameUrl) }); },
      (failure: unknown) => {
        if (!live) return;
        const code = failure instanceof Error ? failure.message : "";
        const refused = code === appPlatformErrorCodes.htmlOff || (failure as { code?: string } | null)?.code === appPlatformErrorCodes.htmlOff;
        setState({ kind: refused ? "off" : "failed" });
      },
    );
    return () => { live = false; };
  }, [adapter.session, fileId, publicFileUrl, policyGeneration]);

  const align = fileBlockAlignment(block.content.align, nested);
  return (
    <figure className={styles.root} contentEditable={false} data-html-embed={state.kind} data-align={align}
      style={{ width: typeof block.content.width === "number" ? `${Math.max(20, Math.min(100, block.content.width))}%` : undefined,
        marginInlineStart: align === "left" ? 0 : "auto", marginInlineEnd: align === "right" ? 0 : "auto" }}>
      <div className={styles.head}>
        <span className={styles.name}>{name}</span>
        <span className={styles.mark} title={blockItemText("htmlEmbed.markHint")}>{blockItemText("htmlEmbed.mark")}</span>
        {/* Notion's "더 보기" over the frame opens the block's menu (seen 2026-10-07). */}
        {!adapter.readOnly ? <button type="button" className={styles.more} aria-label={blockItemText("htmlEmbed.more")} data-html-embed-more
          onClick={(event) => {
            requestEditorBlockCommand(block.id, "menu", blockToolMenuAnchor(event, 0), event.currentTarget);
          }}>
          <DotsHorizontal size="regular" aria-hidden="true" />
        </button> : null}
      </div>
      <div className={styles.frame}>
        {state.kind === "ready" ? (
          <iframe src={state.url} title={name} sandbox="allow-scripts" referrerPolicy="no-referrer" loading="lazy"
            style={{ height: appPlatformBounds.htmlFrameHeight }} data-html-embed-frame />
        ) : (
          <div className={styles.frameState} style={{ height: appPlatformBounds.htmlFrameHeight }} role="status">
            {state.kind === "loading" ? blockItemText("htmlEmbed.loading")
              : state.kind === "off" ? blockItemText("htmlEmbed.off")
                : state.kind === "signedOut" ? blockItemText("embed.publicUnavailable") : blockItemText("htmlEmbed.failed")}
          </div>
        )}
      </div>
    </figure>
  );
}
