import { type ReactNode, useEffect, useRef, useState } from "react";
import type { GraphBlock } from "@/contentGraph";
import { ServiceIcon } from "@/icons/leafen";
import { blockItemText } from "./blockItemLabels";
import { embedBoxFrameWidth, embedBoxWidth, fileBlockAlignment } from "./blockFileMenuModel";
import { BlockCornerHandles, frameShareStyle, PictureHandlePlace, type BlockBoxSize } from "./BlockCornerHandles";
import { keptWidthPatch, useKeptFrameShare } from "./frameKeptWidth";
import { BlockEmptyLinkPane, BlockEmptyPopover, BlockEmptyUploadPane, BlockEmptyRow } from "./BlockEmptyPopover";
import { embedDefaultHeight, embedLinkMakesBookmark, embedServiceForLink, embedServiceFrame, embedServiceOf, ownBlockService, serviceWindowText } from "./embedServices";
import { EmbedUnknownLink } from "./EmbedUnknownLink";
import { emptyRowMark } from "./emptyRowMarks";
import { EmbedLoadFailedCard, embedAddressRefused } from "./EmbedLoadFailedCard";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { ImageBlockTools } from "./ImageBlockTools";
import { blockWebAddress, givenWebLink, unopenedFrameAddress } from "./mediaBlockUrl";
import { useMediaBlockUpload } from "./mediaBlockUpload";
import { useCaptionUnder } from "./mediaCaptionPlace";
import { blockTypeForPastedAssetUrl, PROVIDER_FRAME_ALLOW, providerEmbedUrl, providerFrameSrc, streamingVideoEmbed, WEB_FRAME_REFERRER,
  webFrameSandbox } from "./mediaEmbeds";
import { readLinkedPicture } from "./mediaLinkBlock";
import { LinkedPictureFrame, useFramedKind } from "./LinkedPictureFrame";
import styles from "./editorStyles.generated.module.css";


/**
 * A link's embed: its page in a frame, as Notion's (seen 2026-10-31) — no
 * line, the column wide and 320px high until resized by its sides, foot or
 * lower corners, its tools at the top right on hover. Given its address it
 * stays where its line was, nothing after it and no caret (2026-11-01).
 * One the block API made, or a picture's address made, has Notion's box for
 * it instead (embedBoxWidth, readLinkedPicture).
 */
export function EmbedBlock({ adapter, block, nested = false }: { adapter: NativeEditorGraphAdapter; block: GraphBlock; nested?: boolean }) {
  const storedUrl = typeof block.content.url === "string" ? block.content.url : "";
  // A service's embed from "/ 메뉴" is named for its service, as Notion's
  // "Figma 임베드" (seen 2026-10-01), and draws its links in its frame.
  const service = embedServiceOf(block);
  // The link stays as given, as Notion's; it draws in its service's frame.
  const player = streamingVideoEmbed(storedUrl);
  // A service's embed draws only its service's links; another stays its row with
  // "알 수 없는 URL", as Notion's (seen 2026-11-05: a Figma community file).
  const unknownLink = Boolean(service && storedUrl && !player && !embedServiceFrame(storedUrl, service.id));
  // Its frame opens a web address, one without its scheme over https, never a
  // page of this app (blockWebAddress); words that are no address, which
  // Notion's API keeps, open as one and fail, as Notion's frame does (seen
  // 2026-11-10: "not a url" framed as https://not%20a%20url/).
  const source = player ?? embedServiceFrame(storedUrl, service?.id) ?? providerEmbedUrl(storedUrl) ?? storedUrl;
  const url = unknownLink ? "" : blockWebAddress(source) || unopenedFrameAddress(storedUrl);
  // One whose scheme no frame here loads shows Notion's card instead (embedAddressRefused).
  const refused = Boolean(url) && embedAddressRefused(source);
  // A plain link's frame shows its picture where the address is one, else its page (useFramedKind).
  const framed = useFramedKind(url && !refused && !player && source === storedUrl ? url : "");
  const [draft, setDraft] = useState(storedUrl);
  // A file chosen in its upload tab, or given to a media block, is uploaded here.
  const { upload, error } = useMediaBlockUpload(adapter, block, "embed");
  const figureRef = useRef<HTMLElement>(null), boxRef = useRef<HTMLDivElement>(null);
  const [dragged, setDragged] = useState<BlockBoxSize | null>(null);
  const storedWidth = useKeptFrameShare(figureRef, block.content, "embed");
  const storedHeight = typeof block.content.height === "number" && block.content.height > 0 ? block.content.height : undefined;
  // Made without a size, an embed spans the column 320px high (Notion's, its code and its "/임베드", 2026-10-31);
  // some services otherwise, as Notion's (embedDefaultHeight).
  const width = dragged?.width ?? storedWidth,
    height = dragged?.height ?? storedHeight ?? embedDefaultHeight(service);
  // Made through the block API with no size, it is a box 400 wide, no wider than its column, empty
  // or not (Notion's, seen 2026-11-11: 400 × 336 and 400 × 65; a column 337 wide, 337), where
  // its alignment puts it (fileBlockAlignment: centred under the page, at the start in a toggle).
  const box = dragged ? undefined : embedBoxWidth(block);
  const align = fileBlockAlignment(block.content.align, nested);
  // Its handle and wash go by the empty row, 2px in from the place it keeps (BlockEmptyPopover's sides).
  const boxed = (row: ReactNode) => box ? <div style={{ width: embedBoxFrameWidth(box, 6),
    marginInlineStart: align === "left" ? 0 : "auto", marginInlineEnd: align === "right" ? 0 : "auto" }}>
    <div style={{ marginInline: 2 }}><PictureHandlePlace align={align} /></div>{row}</div> : row;

  // Its caption sits under its frame at its width, in its box (Notion's, seen 2026-11-11).
  useCaptionUnder(figureRef, url);
  useEffect(() => {
    if (url) setDraft(storedUrl);
  }, [storedUrl, url]);

  function commitEmbed(value: string) {
    const normalized = givenWebLink(value);
    if (!normalized) return blockItemText("embed.invalidLink");
    // The service it was made for, or the one the link is.
    const target = service ?? embedServiceForLink(normalized);
    if (embedLinkMakesBookmark(normalized, target?.id)) {
      adapter.replaceBlocks([{ ...block, type: "bookmark", content: { url: normalized } }]);
      return "";
    }
    // A Loom video becomes a video block, in its video's shape, as Notion's (seen 2026-11-05).
    if (target?.id === "loom" && embedServiceFrame(normalized, target.id)) {
      adapter.replaceBlocks([{ ...block, type: "video", content: { url: normalized } }]);
      return "";
    }
    adapter.updateBlock(block, { url: normalized });
    // An address with no file ending is read as the picture's window reads it: an SVG
    // becomes a picture, another picture keeps an embed of its own size (readLinkedPicture).
    if (!target && !blockTypeForPastedAssetUrl(normalized) && !providerEmbedUrl(normalized)) void readLinkedPicture(adapter, block.id, normalized);
    return "";
  }

  if (!url) {
    if (adapter.readOnly) return boxed(service
      ? <BlockEmptyRow name="embed" icon={<ServiceIcon service={service.id} name={service.name} size="regular" />}
        label={blockItemText("embed.serviceTitle", { service: service.rowName ?? service.name })} />
      : <BlockEmptyRow name="embed" icon={emptyRowMark("embed")} label={blockItemText("blockEmpty.embed.row")} />);
    const link = ({ placeholder, submit, hint }: { placeholder: string; submit: string; hint?: string }) => (
      <BlockEmptyLinkPane placeholder={placeholder} submit={submit} hint={hint} initial={draft} onLink={commitEmbed} />
    );
    // A service's embed takes only its link; any other starts on a link and
    // may take a file instead: a picture, video, sound or PDF makes its own
    // block, any other file stays an embed of it (uploadBlockType; Notion, seen 2026-11-07).
    if (service) {
      return boxed(
        <BlockEmptyPopover blockId={block.id} name="embed" label={blockItemText("embed.serviceTitle", { service: service.rowName ?? service.name })}
          icon={<ServiceIcon service={service.id} name={service.name} size="regular" />} phoneTitle={blockItemText("common.embed")}
          variant={unknownLink ? "unknown" : undefined}
          aside={unknownLink ? <EmbedUnknownLink adapter={adapter} blockId={block.id} url={storedUrl} /> : undefined}>
          {() => link(serviceWindowText(service))}
        </BlockEmptyPopover>
      );
    }
    return boxed(
      <BlockEmptyPopover blockId={block.id} name="embed" label={blockItemText("blockEmpty.embed.row")}
        icon={emptyRowMark("embed")} phoneTitle={blockItemText("common.embed")}
        tabs={[{ id: "link", label: blockItemText("blockEmpty.link") }, { id: "upload", label: blockItemText("blockEmpty.upload") }]}
        rowHint={error ? <div className={styles.imageError} role="alert">{error}</div> : null}>
        {(tab) => tab === "link" ? link({ placeholder: blockItemText("blockEmpty.bookmark.placeholder"),
          submit: blockItemText("blockEmpty.embed.submit"), hint: blockItemText("blockEmpty.file.hint") }) : (
          <BlockEmptyUploadPane accept="" choose={blockItemText("blockEmpty.chooseFile")} progress={null}
            onFile={upload} />
        )}
      </BlockEmptyPopover>
    );
  }

  return (
    <figure ref={figureRef} className={styles.embedBlock} contentEditable={false} data-align={align}
      data-sized={width && width < 100 || box ? "true" : undefined} data-resizing={dragged ? "true" : undefined}
      style={width && width !== 100 ? frameShareStyle(width, "embed") : box ? { width: embedBoxFrameWidth(box) } : undefined}>
      <div ref={boxRef} className={`${styles.embedFrame} ${styles.webEmbedFrame}`} data-embed-frame="true" style={{ height }}>
        <PictureHandlePlace align={align} />
        {/* A video provider's player plays only with its address known and its own storage, as in a video block. */}
        {refused ? <EmbedLoadFailedCard />
          : player ? <iframe src={providerFrameSrc(player)} title={String(block.content.title ?? url)} loading="lazy"
            allow={PROVIDER_FRAME_ALLOW} allowFullScreen data-embed-iframe="true" />
          : framed === "picture" ? <LinkedPictureFrame url={url} label={String(block.content.title ?? url)} />
          : !framed ? null
          : <iframe src={url} title={String(block.content.title ?? url)} loading="lazy"
            referrerPolicy={WEB_FRAME_REFERRER} allow="fullscreen; clipboard-write"
            sandbox={webFrameSandbox(url)} data-embed-iframe="true" />}
        {!adapter.readOnly ? <>
          {/* A service's own block shows its fit only once narrower than the column; a plain embed's
              tools keep it across the column (Notion's, seen 2026-11-06: a map and a board without, a drawing with). */}
          <ImageBlockTools adapter={adapter} block={block} mode="web" align={align}
            canAlign={!ownBlockService(block) || (width !== undefined && width < 100) || Boolean(box)} />
          <BlockCornerHandles kind="embed" figureRef={figureRef} boxRef={boxRef} grips="edges" onSize={setDragged} onEnd={(size) => {
            setDragged(null);
            const patch = keptWidthPatch(size, block.content);
            if (size && (patch || size.height !== storedHeight)) adapter.updateBlock(block, { ...patch, height: size.height });
          }} />
        </> : null}
      </div>
    </figure>
  );
}
