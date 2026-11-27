import { useRef } from "react";
import type { GraphBlock, GraphBlockType } from "@/contentGraph";
import { readDatabaseRowPage } from "@/databaseRowPageApi";
import { decodePathPart, pageIdFromPageHref } from "@/lib/pageLinks";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { usePageNames } from "@/lib/pageNames";
import { createUrlMetadataApi } from "@/urlMetadataApi";
import { serverOrigin } from "@/lib/serverOrigin";
import { blockItemText } from "./blockItemLabels";
import { isExternalPastedWebUrl, pastedUrlFallbackTitle } from "./editorInputModel";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { editorBlockChildren } from "./editorBlockIndex";
import { api } from "@/api";
import { blockIsPageItself, blockLinkTarget } from "@/lib/blockAddress";
import { blockTypeForPastedAssetUrl } from "./mediaEmbeds";
import { embedLinkMakesBookmark, embedServiceForLink } from "./embedServices";
import { linkMentionSpan } from "./linkMention";
import type { TextSpan } from "@/lib/types";
import { selectBlockWithoutCaret } from "./slashBlockPlacement";
import styles from "./editorStyles.generated.module.css";
import { keyCameLast } from "@/lib/lastPress";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useOptionalProductToast } from "@/lib/productToast";
import { pasteSyncedCopy } from "./pastedSyncedCopy";
import { placeAround, popoverPlacementStyle, popoverRectAt, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { SlashMenuAnchor } from "./SlashMenu";

/** Where an address was pasted, which its menu's choice acts on (NativeTiptapMenus). */
export type PastedLinkPlace = {
  /**
   * The pasted link as it stands: null once it is gone or changed; `wholeLine`
   * when it is all its line's text (a line of text, not a table's cell).
   */
  find(): { wholeLine: boolean } | null;
  /** Writes these in the pasted link's place, the caret after them; false when the link is gone. */
  write(spans: TextSpan[]): boolean;
};

/**
 * What a link pasted with the caret in a line of text or a table's cell,
 * at its start, its end or between its words, can be instead, as Notion's
 * "붙여넣기 형식" (seen 2026-10-29; every such place 2026-11-11 — none in a
 * caption or code): a link to one of the server's pages, a database item's
 * among them, is mentioned or kept as its URL, and one to a block on such a
 * page can also be its synced copy; a web link is mentioned, kept,
 * bookmarked or embedded, and a link to an image is embedded as one in place
 * of the embed; a mail address is too, and a phone number is mentioned or
 * kept, as Notion's (seen 2026-11-11). An
 * address in the app ("/…", "#…" on this page) is the server's, as Notion's
 * is its own. A heading offers only the mention and the URL. Keeping it as a
 * URL leaves the link as pasted.
 */
export function PastedLinkMenu({ adapter, block, url: pasted, place, onClose, anchor, run = change => change() }: {
  adapter: NativeEditorGraphAdapter;
  /** The line's block, or the table row holding the cell. */
  block: GraphBlock;
  url: string;
  place: PastedLinkPlace;
  onClose: (refocus: boolean) => void;
  run?(change: () => void): void;
  anchor?: SlashMenuAnchor;
}) {
  const pageNames = usePageNames();
  const menuRef = useRef<HTMLDivElement>(null);
  const placement = usePopoverPlacement(menuRef, anchor ? (size, viewport) => placeAround(popoverRectAt(anchor), size, viewport) : null, [anchor], undefined, false);
  // A menu at the caret: a press beside it goes on to the page whole (pressGoesOn), as beside the ":" menu (Notion's
  // own paste menu not brought up in Chrome, 2026-11-26).
  const layer = useOverlayLayer(true, { kind: "caret-menu", ref: menuRef, onEscape: () => onClose(true), onOutsidePress: () => onClose(false) });
  const url = pasted.startsWith("#") ? `${serverOrigin()}/p/${encodeURIComponent(adapter.pageId)}${pasted}`
    : pasted.startsWith("/") ? serverAddress(pasted) : pasted;
  const mail = /^mailto:/iu.test(url), phone = /^tel:/iu.test(url);
  // Only this server's addresses name a page (pageLinks.ts), a database item's among them.
  const ownPageId = pageIdFromPageHref(url);
  const page = ownPageId ? adapter.moveContext?.pages.find((candidate) => candidate.id === ownPageId && !candidate.inTrash) : undefined;
  // An address at the page itself (blockAddress.ts) names the page, not a block on it.
  const pastedBlock = ownPageId ? pastedBlockId(url) : null;
  const linkedBlockId = ownPageId && pastedBlock && !blockIsPageItself(ownPageId, pastedBlock) ? pastedBlock : null;
  const toast = useOptionalProductToast();
  const asset = blockTypeForPastedAssetUrl(url);
  // A heading makes no block of what is pasted in it (Notion's, seen 2026-11-11: "멘션 · URL" for any
  // address); a toggle heading does.
  const makesBlocks = !/^heading_\d$/u.test(block.type);
  function current() {
    return !adapter.readOnly && adapter.isCurrent() && !adapter.signal.aborted && place.find() !== null;
  }
  function mention(spans: TextSpan[]) {
    if (current()) run(() => { place.write(spans); });
  }

  async function mentionPage() {
    if (!ownPageId) return;
    onClose(false);
    // A link to a block mentions the block, named by its words, as Notion's (seen 2026-11-11).
    if (linkedBlockId && adapter.session) {
      const read = await api.readBlockMentionPreview(adapter.session, ownPageId, linkedBlockId).catch(() => null);
      if (read) {
        // A block without words is written as Notion's API reads one.
        mention([{ text: read.words || "Untitled", mention: "page", pageId: ownPageId, blockId: linkedBlockId }, { text: " " }]);
        return;
      }
    }
    let title = page ? pageNames.title(page) : null;
    if (title === null && adapter.session) {
      const row = await readDatabaseRowPage(adapter.session, ownPageId, { limit: 1 }).catch(() => null);
      if (row) title = rowTitle(row.row.values) || untitledPageDisplayTitle("page");
    }
    if (title === null) return void mentionWebPage();
    // A space follows it, the caret after that, between words too, as Notion's (seen 2026-11-11).
    mention([{ text: title, mention: "page", pageId: ownPageId }, { text: " " }]);
  }

  // A web page is mentioned by its title and icon, read from the page,
  // keeping its site's name and preview (linkMention.ts), the caret right
  // after it, as Notion's (seen 2026-11-11).
  // A mail address is mentioned by its domain's site, linking the address,
  // as Notion's (seen 2026-11-11).
  async function mentionWebPage() {
    onClose(false);
    // A phone number is mentioned by itself.
    if (phone) return void mention([linkMentionSpan(url, { title: decodePathPart(url.replace(/^tel:/iu, "")) })]);
    if (!adapter.session) return;
    const site = mail ? mailSite(url) : url;
    const metadata = site ? await createUrlMetadataApi().readUrlMetadata(adapter.session, site)
      .then((response) => response.metadata).catch(() => null) : null;
    const href = !mail && metadata?.url && isExternalPastedWebUrl(metadata.url) ? metadata.url : url;
    mention([linkMentionSpan(href, { ...metadata, title: (metadata?.title || pastedUrlFallbackTitle(site || href)).trim(), provider: metadata?.siteName })]);
  }

  // A link alone on a line of text, with nothing under it, gives way to the
  // block it becomes; anywhere else the link leaves its line, which stays,
  // and the block goes right after the line and what it holds, or after the
  // table of the cell, as Notion's (seen 2026-11-11, an empty list item's,
  // to-do's, toggle's and quote's line too).
  function inPlace() {
    return place.find()?.wholeLine === true && block.type === "paragraph" && editorBlockChildren(adapter.graph, block.id).length === 0;
  }
  // A cell's row stands in its table.
  const after = block.type === "table_row" && block.parentId ? block.parentId : block.id;

  // A link to a block of a page here can bring a synced copy of the block,
  // the block there becoming its original, as Notion's "동기화된 블록" (seen
  // 2026-11-11, on another page or the same one).
  async function syncLinkedBlock() {
    onClose(false);
    if (!ownPageId || !linkedBlockId || !current()) return;
    const spot = inPlace() ? [block.id] : { after, first: () => current() && place.write([]) };
    const parentId = adapter.graph.blocks.find(candidate => candidate.id === after)?.parentId ?? null;
    const there = new Set(editorBlockChildren(adapter.graph, parentId).map(candidate => candidate.id));
    const made = await pasteSyncedCopy(adapter, { pageId: ownPageId, blockIds: [linkedBlockId] }, spot, current, run)
      .catch(() => "failed" as const);
    if (made === "failed") toast?.notify(blockItemText("pastedBlocks.syncFailed"), "error");
    if (made === "unreachable") toast?.notify(blockItemText("pastedBlocks.syncUnreachable"), "error");
    // The copy is chosen, as a block made from the menu is (seen 2026-11-11).
    const copy = made === "made" ? editorBlockChildren(adapter.graph, parentId)
      .find(candidate => !there.has(candidate.id) && candidate.type === "synced_block" && candidate.content.syncedBlockId) : undefined;
    if (copy) selectBlockWithoutCaret(copy.id);
  }

  // The block made is chosen, no caret anywhere (Notion's, seen 2026-11-11).
  function becomeBlock(type: GraphBlockType, content: Record<string, unknown>) {
    if (!current()) return;
    onClose(false);
    run(() => {
      let made: string | undefined;
      if (inPlace()) {
        adapter.changeText(block, "");
        adapter.replaceBlocks([{ ...block, type, content }]);
        made = block.id;
      } else if (place.write([])) made = adapter.insertAfter(after, type, content)?.id;
      if (made) selectBlockWithoutCaret(made);
    });
  }

  const actions: Array<{ id: string; label: string; choose(): void }> = [];
  function option(id: string, label: string, choose: () => void) { actions.push({ id, label, choose }); }
  if (ownPageId) option("page-mention", blockItemText("mention.label"), () => void mentionPage());
  else if (mail || phone || isExternalPastedWebUrl(url)) option("external-mention", blockItemText("mention.label"), () => void mentionWebPage());
  if (makesBlocks && ownPageId && linkedBlockId) option("synced-block", blockItemText("synced.block"), () => void syncLinkedBlock());
  // The rest as Notion's (seen 2026-11-05): a link to a picture, video, sound or PDF, or a
  // streaming site's, embeds as that block; any other, a zip's too, only as a frame, a named
  // service's under its name. YouTube's, Figma's and Miro's keep URL for last. Escape and "URL" leave the link as it was pasted;
  // after Escape or "URL" chosen with ↓ and Enter the caret is after it (NativeTiptapMenus refocuses the page), as Notion's
  // (2026-11-15, worker Chrome, no script before the key: a letter typed next went in right after the link, in both).
  // "URL" pressed with the mouse lets go of the focus instead, the page's body holding it and a letter typed next
  // going nowhere, as Notion's (measured twice on 2026-11-15 in Chrome 1440 in different tabs, no script before the
  // press, the screen and one typed letter after it).
  const keepUrl = () => option("url", blockItemText("pastedLink.url"), () => onClose(keyCameLast()));
  // A named service's link embeds under its name, before "북마크" (embedServices.ts paste);
  // Loom's, a streaming site's, is the video block it makes.
  const service = makesBlocks && !ownPageId ? embedServiceForLink(url) : undefined;
  const named = service?.paste?.name;
  const namedEmbed = named ? blockItemText("pastedLink.namedEmbed", {
    name: named === "miroBoard" ? blockItemText("pastedLink.miroBoard") : named }) : null;
  const keepsUrlLast = makesBlocks && !ownPageId && (/(?:^|\.)(?:youtube\.com|youtu\.be)$/iu.test(linkHost(url)) || service?.paste?.urlLast === true);
  if (!keepsUrlLast) keepUrl();
  if (makesBlocks && !ownPageId && !phone) {
    if (asset === "image") option("image", blockItemText("pastedLink.createImage"), () => becomeBlock("image", { url }));
    if (asset === "video") option("video", namedEmbed ?? blockItemText("blockEmpty.video.submit"), () => becomeBlock("video", { url }));
    if (asset === "audio") option("audio", blockItemText("blockEmpty.audio.submit"), () => becomeBlock("audio", { url }));
    if (asset === "pdf") option("pdf", blockItemText("blockEmpty.pdf.submit"), () => becomeBlock("pdf", { url }));
    // A link its site won't let be framed is a bookmark however it is embedded (embedLinkMakesBookmark).
    const embed = () => option("embed", namedEmbed ?? blockItemText("common.embed"),
      () => becomeBlock(embedLinkMakesBookmark(url) ? "bookmark" : "embed", { url }));
    if (!asset && namedEmbed) embed();
    option("bookmark", blockItemText("bookmark.label"), () => becomeBlock("bookmark", { url }));
    if (!asset && !namedEmbed) embed();
  }
  if (keepsUrlLast) keepUrl();
  const picker = useNativeMenu({ items: actions, open: true, menuRef, label: blockItemText("pastedLink.options"),
    keyOf: item => item.id, textOf: item => item.label, onAction: item => item.choose(),
    // Its first choice is ready for Enter, as Notion's (seen 2026-11-11: "멘션" shown chosen).
    menuOptions: { autoFocus: "first" } });
  // The page keeps the focus while it is open, so its lit row is shaded and
  // never ringed, however the paste or the arrows in it came (`data-ringless`,
  // NativeMenuItem), as Notion's (seen 2026-11-16 at 1440 in Chrome: pasted by
  // ⌘V after letters typed, after a press in the line, or after ↑ and End,
  // the lit row grey with no ring, and ↓ moved the grey on, unringed).
  return (
    <div {...picker.menuProps} ref={menuRef} className={styles.pastedUrlMenu} data-ringless="true"
      style={{ ...popoverPlacementStyle(placement), zIndex: layer.zIndex }} contentEditable={false}
      onMouseDown={(event) => event.preventDefault()} onKeyDown={event => {
        picker.menuProps.onKeyDown?.(event); layer.onKeyDown(event); event.stopPropagation();
      }}>
      <div className={styles.pastedUrlTitle}>{blockItemText("pastedLink.pasteAs")}</div>
      {actions.map(item => <NativeMenuItem key={item.id} state={picker.state} itemKey={item.id} data-pasted-url-option>
        {({ labelProps }) => <span {...labelProps}>{item.label}</span>}
      </NativeMenuItem>)}
    </div>
  );
}

function linkHost(url: string) {
  try {
    return new URL(url).hostname;
  } catch {
    return "";
  }
}

/** An address in the app ("/…") as the server's web address. */
function serverAddress(path: string) {
  try {
    return new URL(path, serverOrigin()).toString();
  } catch {
    return path;
  }
}

/** The web address of a mail address's domain ("mailto:a@b.co": https://b.co), if any. */
function mailSite(url: string) {
  const domain = /^mailto:[^@?#]+@([^?#/\s]+)/iu.exec(url)?.[1];
  return domain ? `https://${domain}` : "";
}

/** The block a pasted link to a page names in its fragment, if any. */
function pastedBlockId(url: string) {
  try {
    return blockLinkTarget(new URL(url, window.location.href).hash);
  } catch {
    return null;
  }
}

/** A database item's title, from its row's values. */
function rowTitle(values: Record<string, { type: string; value: unknown }>) {
  const title = Object.values(values).find((value) => value.type === "title")?.value;
  return typeof title === "string" ? title.trim() : "";
}
