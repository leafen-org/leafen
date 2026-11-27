import { useEffect, useMemo, useRef, useState } from "react";
import { appPlatformBounds, appPlatformErrorCodes } from "@/appPlatformWire.generated";
import { appText } from "@/components/admin/administratorAppsApi";
import { AppPlaceFrame } from "@/components/apps/AppPlaceFrame";
import { appPlaceName, AppUnavailableNotice, useAppAvailability } from "@/components/apps/AppUnavailable";
import { readUsableAppsNow, usableAppFor, useUsableAppList } from "@/components/apps/useUsableApps";
import { useProductPages } from "@/components/product/productPagesContext";
import type { GraphBlock } from "@/contentGraph";
import { i18next } from "@/i18n";
import { DotsHorizontal } from "@/icons/leafen";
import { blockItemText } from "./blockItemLabels";
import { blockToolMenuAnchor, requestEditorBlockCommand } from "./editorCommands";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./AppEmbedBlock.module.css";
import { appBridgeRefusal } from "@/components/apps/appFrameBridge";
import { announceAppsChanged } from "@/lib/appsChangedEvents";

// An app's block in a page (docs/app-platform.md 2): an embed whose
// service is "app", drawn by the app in a frame of its own under Leafen's
// head naming the block, marked as an app's with the app's name so no app
// passes as Leafen or as another. The app reads the page and the block, keeps
// its settings in the block while the page may be edited, and sets its
// height. A published page, or someone who may not use the app, sees the
// block's name in its place.

const MIN_HEIGHT = 48;
const MAX_HEIGHT = 2000;

export function AppEmbedBlock({ adapter, block }: { adapter: NativeEditorGraphAdapter; block: GraphBlock }) {
  const appId = String(block.content.app ?? "");
  const blockKey = String(block.content.block ?? "");
  const savedName = String(block.content.name ?? "");
  const props = block.content.props && typeof block.content.props === "object" ? block.content.props as Record<string, unknown> : {};
  const propsJson = JSON.stringify(props);
  const session = adapter.session;
  const organizationId = useProductPages()?.organizationId;
  const { apps, loaded } = useUsableAppList(session, Boolean(session));
  const usable = usableAppFor(apps, appId, organizationId);
  const declared = usable?.blocks?.find((candidate) => candidate.key === blockKey);
  const language = i18next.language;
  const name = declared ? appText(declared.name, language) : savedName || blockKey;
  const [height, setHeight] = useState(0);
  const shownHeight = height || declared?.defaultHeight || appPlatformBounds.blockDefaultHeight;
  const port = useRef<MessagePort | null>(null);

  // Settings changed elsewhere — by someone else, or undone — reach the app.
  useEffect(() => {
    port.current?.postMessage({ type: "props", props: JSON.parse(propsJson) as unknown });
  }, [propsJson]);

  const context = () => ({ pageId: adapter.pageId, blockId: block.id, block: blockKey, props: JSON.parse(propsJson) as unknown,
    editable: !adapter.readOnly });
  const extra = useMemo(() => async (method: string, params: Record<string, unknown>) => {
    if (method === "resize") {
      const asked = Math.round(Number(params.height));
      if (!Number.isFinite(asked)) throw appBridgeRefusal("invalidCall");
      setHeight(Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, asked)));
      return null;
    }
    if (method === "blockProps") {
      // Kept in the block, so only someone who may edit the page changes them.
      if (adapter.readOnly) throw appBridgeRefusal("readOnly");
      const next = params.props;
      if (!next || typeof next !== "object" || Array.isArray(next)) throw appBridgeRefusal("invalidCall");
      if (new TextEncoder().encode(JSON.stringify(next)).length > appPlatformBounds.blockPropsMaxBytes) throw appBridgeRefusal("tooLarge");
      // A stopped or removed app writes nothing more from a block left open:
      // the list is read again first, and the call is refused as the server
      // refuses a stopped app's, which turns every open place to the notice.
      if (session) {
        const now = await readUsableAppsNow(session).catch(() => null);
        if (now && !usableAppFor(now, appId, organizationId)?.blocks?.some(({ key }) => key === blockKey)) {
          announceAppsChanged();
          throw new Error(`${appPlatformErrorCodes.appStopped}: The app is stopped or no longer the person's to use here.`);
        }
      }
      const live = adapter.graph.blocks.find(({ id }) => id === block.id);
      if (!live) throw appBridgeRefusal("blockGone");
      adapter.updateBlock(live, { props: next });
      return null;
    }
    throw appBridgeRefusal("unknownCall");
  }, [adapter, appId, block.id, blockKey, organizationId, session]);

  // Someone who may not use the app here is told why and what to do; a
  // published page shows no app (2026-10-16: both said one vague sentence).
  const missing = Boolean(session) && loaded && !declared;
  const availability = useAppAvailability(session, { app: appId }, organizationId ? `org:${organizationId}` : "", missing);
  // Its mark names the app while it can't run here too: "앱 · 〈앱〉", not "앱".
  const appName = appPlaceName(usable, missing ? availability : null, language);
  const unavailable = !session ? blockItemText("appBlock.signedOut") : missing ? "unavailable" : "";
  return (
    <figure className={styles.root} contentEditable={false} data-app-block={appId} data-app-block-key={blockKey}>
      <div className={styles.head}>
        <span className={styles.name}>{name}</span>
        <span className={styles.mark} title={blockItemText("appBlock.markHint")} data-app-block-mark>
          {appName ? blockItemText("appBlock.markNamed", { app: appName }) : blockItemText("appBlock.mark")}
        </span>
        <button type="button" className={styles.more} aria-label={blockItemText("appBlock.more")} data-app-block-more
          onClick={(event) => {
            requestEditorBlockCommand(block.id, "menu", blockToolMenuAnchor(event, 0), event.currentTarget);
          }}>
          <DotsHorizontal size="regular" aria-hidden="true" />
        </button>
      </div>
      <div className={styles.frame}>
        {session && usable && declared ? (
          <AppPlaceFrame session={session} usable={usable} place="block" opens={{ surface: "block", block: blockKey }} title={name}
            style={{ height: shownHeight }} context={context} extra={extra} port={port} />
        ) : (
          <div className={styles.frameState} role="status" data-app-block-state={unavailable ? "unavailable" : "loading"}>
            {missing ? <AppUnavailableNotice availability={availability} session={session} compact onRetry={announceAppsChanged} />
              : !session ? (
                <span>
                  {blockItemText("appBlock.signedOut")}{" "}
                  <a href={`/p/${encodeURIComponent(adapter.pageId)}`} data-app-block-open-in-leafen>{blockItemText("appBlock.openInLeafen")}</a>
                </span>
              ) : blockItemText("appBlock.loading")}
          </div>
        )}
      </div>
    </figure>
  );
}
