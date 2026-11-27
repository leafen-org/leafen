import {
  type SyntheticEvent,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import type { GraphBlock } from "@/contentGraph";
import { isManagedFileBlock } from "@/contentGraph";
import { FileText, VideoUnplayableIcon, WarningCircleIcon } from "@/icons/leafen";
import { emptyRowMark } from "./emptyRowMarks";
import { fileNameFromUrl } from "../database/files";
import { blockItemText } from "./blockItemLabels";
import { BlockCornerHandles, frameShareStyle, PictureHandlePlace } from "./BlockCornerHandles";
import { ImageBlockTools } from "./ImageBlockTools";
import { ImageAltBadge, ImageAltTextWindow } from "./ImageAltText";
import { ImageLinkAnchor, ImageLinkLabel } from "./ImageLinkLabel";
import { useImageLoadFailure } from "@/lib/imageLoadFailure";
import { ImageLoadFailure } from "./ImageLoadFailure";
import { CoverCredit } from "../page/CoverCredit";
import { ManagedEmbedBlock } from "./ManagedEmbedBlock";
import { EmbedBlock } from "./LinkEmbedBlock";
import { AppEmbedBlock } from "./AppEmbedBlock";
import { HtmlEmbedEmpty, HtmlEmbedFrame } from "./HtmlEmbedBlock";
import { normalizedFileContentType } from "@/lib/fileSecurity";
import { sandboxedHtmlContentType } from "@/fileSecurityPolicy.generated";
import { ManagedFileBlock } from "./ManagedFileBlock";
import { fileKindIcon, fileRowKindOf } from "./fileKindIcon";
import { useBlockDocumentApp } from "./useBlockDocumentApp";
import { emptyText, MediaUploadForm, UploadingMedia } from "./MediaUploadForm";
import { blockUploadRunning, useBlockUploadJob, useTakeFinishedBlockUpload } from "./mediaBlockUpload";
import { PdfBlock } from "./PdfBlock";
import { fileBlockAlignment, uploadedFrameBox } from "./blockFileMenuModel";
import { imageAlignPlaces, uploadSpansColumn } from "./imagePlacement";
import { keptWidthPatch, useKeptFrameShare } from "./frameKeptWidth";
import { blockTypeForPastedAssetUrl, PROVIDER_FRAME_ALLOW, providerFrameSrc, streamingVideoEmbed, WEB_FRAME_REFERRER, webFrameSandbox } from "./mediaEmbeds";
import { useStreamedVideoShape } from "./streamedVideoShape";
import { useVideoPoster } from "./videoPoster";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./editorStyles.generated.module.css";
import { desktopBackgroundAvailable } from "@/lib/desktopBackground";
import { touchScreen } from "@/lib/screenKind";
import { useAppleVideoHold } from "./appleVideoHold";
import { giveMediaLink, linkedPictureBox } from "./mediaLinkBlock";
import { givenWebLink, safeMediaUrl, unopenedFrameAddress, useColumnPreviewWidth, useMediaBlockUrl } from "./mediaBlockUrl";
import { requestEditorBlockCommand } from "./editorCommands";
import { useCaptionUnder } from "./mediaCaptionPlace";
import { useBlockFileActions } from "./useBlockFileActions";
import { BlockEmptyRow } from "./BlockEmptyPopover";
import { ReaderImagePreview } from "./BlockImagePreview";
import { BookmarkBlock, LinkPreviewBlock } from "./BookmarkBlock";
export { safeMediaUrl, useMediaBlockUrl } from "./mediaBlockUrl";

/** A picture may go past the column, as far as this share of it. */
const PICTURE_MOST = 1000;

/** A picture's pixel size as its block keeps it; 0 for a side not kept. */
function keptPixelSide(value: unknown) {
  return typeof value === "number" && Number.isInteger(value) && value > 0 && value <= 100000 ? value : 0;
}


/** A media block's external address as kept, trimmed; "" for none. */
function storedMediaAddress(block: GraphBlock) {
  return typeof block.content.url === "string" ? block.content.url.trim() : "";
}



export function ImageBlock({ adapter, block, nested = false }: { adapter: NativeEditorGraphAdapter; block: GraphBlock; nested?: boolean }) {
  const figureRef = useRef<HTMLElement>(null);
  // Its width as its side handles left it, a share of its column as drawn now (useKeptFrameShare).
  const storedWidth = useKeptFrameShare(figureRef, block.content, "image", PICTURE_MOST);
  const frameRef = useRef<HTMLDivElement>(null);
  const [draggedWidth, setDraggedWidth] = useState<number | null>(null);
  // Never sized, an uploaded picture shows 32px narrower than it is, the column
  // at most, as Notion's (2026-11-01: 40 → 8, 300 → 268, 480 → 448), or across it once
  // wider (uploadSpansColumn). A preview comes at least twice the column wide, so its own width serves.
  const [naturalWidth, setNaturalWidth] = useState(0);
  // The photo comes at the size its share of the column shows it, not as the
  // original's megabytes (2026-10-17).
  const shown = Boolean(isManagedFileBlock(block) ? block.content.fileId : safeMediaUrl(block.content.url));
  const previewWidth = useColumnPreviewWidth(figureRef, storedWidth ?? 100, shown);
  const { managed, url, refused } = useMediaBlockUrl(block, previewWidth);
  const failure = useImageLoadFailure(url, refused);
  const imageWidth = draggedWidth ?? storedWidth ?? (uploadSpansColumn(block, managed, naturalWidth) ? 100 : undefined);
  // A linked picture never sized shows as it is, at the column's start; an
  // uploaded one 32px narrower in the middle (Notion's, seen 2026-11-06: the
  // same 172×178 picture linked drawn 172 at the left, pasted 140 centered),
  // and a linked one made narrower goes to the middle too (seen 2026-11-06).
  // An imported picture is drawn as a linked one (Notion's import, seen
  // 2026-11-07: a ZIP's 40px picture drawn 40 at the left), and under a
  // picture at its own size the caption takes the block's width. With no
  // alignment chosen its tools show the middle, as Notion's (seen 2026-11-14
  // and 2026-11-15, linked by "/이미지" and through the API); inside another
  // block it stands at that block's start (fileBlockAlignment).
  const ownSize = !managed || block.content.naturalSize === true;
  const chosenAlign = fileBlockAlignment(block.content.align, nested);
  // One an SVG link made keeps Notion's box (linkedPictureBox), where its alignment puts it, the
  // picture at its start no wider than the box less its sides, at its own size where narrower.
  const box = imageWidth ? undefined : linkedPictureBox(block);
  // Given an alignment while never sized, such a picture still stands at the
  // column's start, its tools showing the choice, which it takes once sized,
  // as Notion's (seen 2026-11-11 and 2026-11-12: a 172px link set to the right
  // or the middle stayed at 496..668; dragged narrower, it went to the right).
  const imageAlign = ownSize && !imageWidth && !box ? "left" : chosenAlign;
  const naturalShown = naturalWidth ? Math.max(1, ownSize ? naturalWidth : naturalWidth - 32) : 0;
  // Placed again when the picture's alignment or kept width changes: a
  // picture dragged narrower goes to the middle when let go, without its box
  // changing size then (2026-11-11: the caption stayed at the block's start).
  // One at its own size at the column's start, linked too, has its caption
  // across the block, its "ALT" still at its own corner; one uploaded and shown
  // 32px narrower, or sized, at its width (Notion's, seen 2026-11-16 at 1440 in
  // two tabs: linked 200 and 300 and an API upload 200 across 495.5..1199.5, a
  // 200 uploaded in the page shown 168, pictures sized 104 and 272 at theirs).
  useCaptionUnder(frameRef, `${url}\n${imageAlign}\n${storedWidth ?? ""}`, ownSize && !imageWidth && !box);
  // A picture whose file is still on its way keeps the waiting place, not
  // none: on a slow or stopped network it vanished from the page, and the
  // page moved when it came (2026-11-07). One already here takes its own
  // size before the first paint.
  const [loadedUrl, setLoadedUrl] = useState("");
  const [altOpen, setAltOpen] = useState(false);
  // Where the page can't be edited, a single press on the picture shows it full screen, as Notion's
  // (seen 2026-11-15 on a locked page, at its middle and its edge; the caption opens nothing). Where it
  // can, the press chooses the block (seen the same day, twice).
  const [readerPreview, setReaderPreview] = useState(false);
  const altBadgeRef = useRef<HTMLButtonElement>(null);
  const imageRef = useRef<HTMLImageElement | null>(null);
  useLayoutEffect(() => {
    if (url && imageRef.current?.complete && imageRef.current.naturalWidth > 0) setLoadedUrl(url);
  }, [url]);
  const waiting = (!url && !refused) || (Boolean(url) && url !== loadedUrl && !failure.failed);
  // The picture's pixel size, kept on the block once it was drawn, holds its
  // place before its file comes, as Notion's block keeps its picture's shape
  // (block_aspect_ratio): the page under it doesn't move when it comes
  // (2026-11-07: its 16:9 waiting place grew to the picture). A picture kept
  // before that, or put in another's place, keeps its size once drawn.
  const pixelWidth = keptPixelSide(block.content.pixelWidth), pixelHeight = keptPixelSide(block.content.pixelHeight);
  const pixelSized = pixelWidth > 0 && pixelHeight > 0;
  const heldPlace = {
    ...(waiting && pixelSized ? { aspectRatio: `${pixelWidth} / ${pixelHeight}`, minHeight: 0,
      ...(imageWidth ? {} : { width: Math.max(1, ownSize ? pixelWidth : pixelWidth - 32) }) } : {}),
    ...(box ? { marginInlineStart: 2, marginInlineEnd: "auto" } : {}),
  };
  const keepPixelSize = (image: HTMLImageElement) => {
    const width = image.naturalWidth, height = image.naturalHeight;
    if (adapter.readOnly || !width || !height || adapter.signal.aborted || !adapter.isCurrent()) return;
    if (pixelSized && Math.abs(pixelWidth / pixelHeight - width / height) < 0.01) return;
    const live = adapter.graph.blocks.find((candidate) => candidate.id === block.id);
    if (!live || live.type !== "image" || (live.content.fileId ?? live.content.url) !== (block.content.fileId ?? block.content.url)) return;
    const size = { pixelWidth: width, pixelHeight: height };
    if (adapter.fillBlock) adapter.fillBlock(live, size);
    else adapter.updateBlock(live, size);
  };
  // An address kept that opens nothing, as Notion's API keeps "mailto:…",
  // fails to load, as Notion's picture does (seen 2026-11-10).
  if (!url && !managed && storedMediaAddress(block)) {
    return <figure ref={figureRef} className={styles.imageBlock} data-align={imageAlign}>
      <div ref={frameRef} className={styles.imageFrame} data-failed="true"><ImageLoadFailure url="about:blank" /></div></figure>;
  }
  if (!url && !managed) {
    if (adapter.readOnly) return <BlockEmptyRow name="image" icon={emptyRowMark("image")} label={emptyText("image").row} />;
    return <MediaUploadForm adapter={adapter} block={block} kind="image" accept="image/*" icon={emptyRowMark("image")}
      initialLink={url} onLink={(value) => giveMediaLink(adapter, block, value, "image.invalidLink")} />;
  }

  // The alt text the block menu saves, or one an import brought as `alt`.
  const alt = String(block.content.altText ?? block.content.alt ?? "");
  const link = typeof block.content.imageLink === "string" ? safeMediaUrl(block.content.imageLink) : "";
  const places = imageAlignPlaces({ sized: imageWidth, ownSize, shown: naturalShown, column: figureRef.current?.clientWidth ?? 0 });
  const image = url && previewWidth ? <img ref={imageRef} src={url} alt={alt} onError={failure.fail}
    onClick={adapter.readOnly && !link ? () => setReaderPreview(true) : undefined}
    onLoad={(event) => { setNaturalWidth(event.currentTarget.naturalWidth); setLoadedUrl(url); keepPixelSize(event.currentTarget); }}
    style={!imageWidth && naturalShown ? { width: naturalShown } : undefined} /> : null;
  return <figure ref={figureRef} className={styles.imageBlock} data-sized={imageWidth ? "true" : undefined} data-align={imageAlign}
    data-resizing={draggedWidth !== null ? "true" : undefined} data-image-fit={places.menu ? "true" : undefined}
    style={frameShareStyle(imageWidth) ?? (box ? { width: `min(${box - 12}px, 100%)` } : undefined)}><div ref={frameRef}
      className={styles.imageFrame} aria-busy={waiting || undefined}
      data-pending={waiting ? "true" : undefined} data-failed={failure.failed ? "true" : undefined} style={heldPlace}>
      {failure.failed
        ? <ImageLoadFailure url={url} refused={refused} session={adapter.session} />
        : image && link ? <ImageLinkAnchor href={link}>{image}</ImageLinkAnchor> : image}
      {image && link && !failure.failed ? <ImageLinkLabel adapter={adapter} blockId={block.id} href={link} /> : null}
      {/* An uploaded picture's box hugs it at any alignment, as Notion's
          (seen 2026-11-12: a 400px upload washed 380 wide set left, in the
          middle or right); one at its own size never sized keeps the row. */}
      <PictureHandlePlace align={imageAlign} hug={!ownSize || !!imageWidth || !!box} />
      {image && alt && !failure.failed && !adapter.readOnly ? <ImageAltBadge alt={alt} buttonRef={altBadgeRef} onOpen={() => setAltOpen(true)} /> : null}
      {altOpen && altBadgeRef.current && !adapter.readOnly ? <ImageAltTextWindow anchor={{ badge: altBadgeRef.current }} alt={alt}
        onClose={() => setAltOpen(false)} onSave={(text) => {
          const live = adapter.graph.blocks.find((candidate) => candidate.id === block.id);
          if (live) adapter.updateBlock(live, { altText: text.trim() || undefined });
        }} /> : null}
      {image && !failure.failed ? <CoverCredit cover={url} /> : null}
      {readerPreview && url && adapter.readOnly ? <ReaderImagePreview block={block} session={adapter.session} url={url}
        onClose={() => setReaderPreview(false)} /> : null}
      {url && !adapter.readOnly ? <>
        {/* Its side pills size it, its block's px kept once the drag ends (BlockCornerHandles, keptBlockWidth);
            a picture that couldn't be loaded has none, its tools alone, as Notion's (seen 2026-11-14 and 2026-11-15). */}
        {failure.failed ? null : <BlockCornerHandles kind="image" figureRef={figureRef} boxRef={frameRef} grips="sides"
          onSize={(size) => setDraggedWidth(size.width)} onEnd={(size) => {
            setDraggedWidth(null);
            const patch = keptWidthPatch(size, block.content); if (patch) adapter.updateBlock(block, patch);
          }} />}
        {/* One at its own size keeps its alignment choice even as wide as its column, as Notion's
            (seen 2026-11-06: a 1600px link; 2026-11-27: a 1600px block API upload); one uploaded
            in the page that wide has none (seen 2026-11-05), nor "맞춤" in its menu (imageAlignPlaces).
            They fold to "⋯" by its block's room, or by its own width where its upload drew it narrower (useMediaToolsFold). */}
        <ImageBlockTools adapter={adapter} block={block} align={chosenAlign} canAlign={places.tools} foldRoom={!ownSize && !imageWidth && !box ? "frame" : "block"} />
      </> : null}
    </div></figure>;
}

/**
 * The address a player plays. A file's signed address changes every few
 * minutes, and a new src starts a video over, stopped (seen 2026-11-05: a video
 * went back to 0:00 as it played). So a player keeps the address it has; only
 * when that one is refused, by then expired, does it take the newest and go on
 * from where it was.
 */
function usePlayerSource(identity: string, url: string) {
  const [held, setHeld] = useState({ identity, url });
  if (held.identity !== identity || (!held.url && url)) setHeld({ identity, url });
  const source = held.identity === identity && held.url ? held.url : url;
  const playing = useRef(false);
  const resume = useRef<{ time: number; playing: boolean } | null>(null);
  return {
    source,
    newer: Boolean(url) && url !== source,
    /** Goes on from the newest address, if there is one newer than the refused one. */
    recover(player: HTMLMediaElement) {
      if (!url || url === source) return false;
      resume.current = { time: player.currentTime, playing: playing.current };
      setHeld({ identity, url });
      return true;
    },
    events: {
      onPlay: () => { playing.current = true; },
      onPause: () => { playing.current = false; },
      onLoadedMetadata: (event: SyntheticEvent<HTMLMediaElement>) => {
        const was = resume.current;
        resume.current = null;
        if (!was) return;
        event.currentTarget.currentTime = was.time;
        if (was.playing) void event.currentTarget.play().catch(() => {});
      },
    },
  };
}

/**
 * An audio file that will not play says so under its player in
 * Notion's words, naming its kind (seen 2026-11-11: "이 오디오 형식(mp3)은…",
 * "(알 수 없음)" for "mailto:…"), then what to do: replace it, or open the
 * original where there is one to open.
 */
function audioFormatFailure(extension: string, original: boolean) {
  return blockItemText(original ? "media.audioFormatUnplayable" : "media.audioFormatUnplayableNoOriginal",
    { extension: extension || blockItemText("media.unknownFormat") });
}

export function MediaBlock({ adapter, block, nested = false }: { adapter: NativeEditorGraphAdapter; block: GraphBlock; nested?: boolean }) {
  const { managed, url: latestUrl } = useMediaBlockUrl(block);
  // A video's kept address that opens nothing is framed as a page that will
  // not open, as Notion frames it (seen 2026-11-11: "mailto:…" in its 4:3
  // web page frame, 704 × 528), as an embed's is (unopenedFrameAddress).
  const unopenedVideo = block.type === "video" && !managed && !latestUrl ? unopenedFrameAddress(storedMediaAddress(block)) : "";
  const shownUrl = latestUrl || unopenedVideo;
  const playerSource = usePlayerSource(managed ? String(block.content.fileId ?? "") : shownUrl, shownUrl);
  const url = playerSource.source;
  const fileActions = useBlockFileActions(block, adapter.session);
  // "format" when the browser cannot play the kind of file, "load" for any other failure.
  const [playbackError, setPlaybackError] = useState<false | "format" | "load">(false);
  const [started, setStarted] = useState(false);
  // Whether the video's own shape has come (MediaBlock's player, EditorMediaBlocks.module.css).
  const [shaped, setShaped] = useState(false);
  const playbackErrorId = useId();
  const video = block.type === "video";
  const playerFigureRef = useRef<HTMLElement>(null);
  const frameRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<HTMLVideoElement>(null);
  const audioRef = useRef<HTMLAudioElement>(null);
  const [draggedWidth, setDraggedWidth] = useState<number | null>(null);
  const storedWidth = useKeptFrameShare(playerFigureRef, block.content, video ? "video" : "audio", video ? PICTURE_MOST : 100);
  // A pressed video keeps its picture clear until the pointer comes back to it, as Notion's (seen 2026-11-05).
  const [pressed, setPressed] = useState(false);
  // An iPhone or iPad draws the video's bar before it plays, as Notion's there (useAppleVideoHold).
  const hold = useAppleVideoHold();
  useCaptionUnder(playerFigureRef, url);
  const streamedShape = useStreamedVideoShape(adapter, block, video && !managed && Boolean(url && streamingVideoEmbed(url)));
  const shownFile = managed ? String(block.content.fileId ?? "") : url;
  // An uploaded video shows a still from about its first second before it plays, as Notion's (videoPoster).
  const poster = useVideoPoster({ fileId: managed && video ? shownFile : "", url, session: adapter.session, editable: !adapter.readOnly });
  useEffect(() => { setPlaybackError(false); setStarted(false); setShaped(false); }, [shownFile]);
  // A refusal that came before the next address did: it goes on when that comes.
  useEffect(() => {
    const element = videoRef.current ?? audioRef.current;
    if (playbackError && playerSource.newer && element && playerSource.recover(element)) setPlaybackError(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [playbackError, playerSource.newer]);
  // Its place held while its address comes, so the page doesn't move under the reader (seen 2026-11-05).
  if (managed && !url) return <figure className={video ? styles.videoBlock : styles.audioBlock} contentEditable={false} data-file-source="managed" aria-busy="true">
    <div className={video ? styles.videoFrame : styles.audioFrame}>
      {video ? <div className={styles.videoPlayer} data-shape="unknown" /> : <div className={styles.audioPlaceholder} />}
    </div>
  </figure>;
  // An audio's address kept that opens nothing can't play, as Notion's player
  // says (seen 2026-11-10, "mailto:…": its kind unknown; 2026-11-11, an http
  // one's named by its end: "https://exa mple.com/a.mp3", mp3); there is no original to open.
  // The desktop app's player stands as Notion's app draws it (.audioPlayer).
  const appPlayer = desktopBackgroundAvailable() && !touchScreen();
  const unopenedAudio = !url && !managed ? storedMediaAddress(block) : "";
  if (unopenedAudio) {
    return <figure className={styles.audioBlock} contentEditable={false} data-file-source="external">
      <div className={styles.audioFrame} data-playback-error="true">
        <audio className={styles.audioPlayer} controls preload="none" data-app-player={appPlayer ? "" : undefined} />
        <div className={styles.mediaPlaybackError} role="alert">
          {audioFormatFailure(/^https?:/iu.test(unopenedAudio) ? mediaExtension(unopenedAudio, undefined) : "", false)}</div>
      </div>
    </figure>;
  }
  if (!url) {
    if (adapter.readOnly) return <BlockEmptyRow name={video ? "video" : "audio"} label={emptyText(video ? "video" : "audio").row}
      icon={emptyRowMark(video ? "video" : "audio")} />;
    return <MediaUploadForm adapter={adapter} block={block} kind={video ? "video" : "audio"} accept={video ? "video/*" : ""}
      icon={emptyRowMark(video ? "video" : "audio")} initialLink={url}
      onLink={(value) => giveMediaLink(adapter, block, value, video ? "media.invalidVideoLink" : "media.invalidAudioLink")} />;
  }
  // A video from a streaming site plays in that site's player; a link that is
  // no video file shows its page in a 4:3 frame, as Notion's (seen 2026-11-05: 704×528).
  const streamed = video && !managed ? streamingVideoEmbed(url) : null;
  const webPage = video && !managed && !streamed && blockTypeForPastedAssetUrl(url) !== "video";
  // Notion reads a video's first frame but no part of an audio file until it plays
  // (seen 2026-11-06: preload "metadata" and "none", the audio bar "0:00 / 0:00"); a video plays inline, as Notion's.
  const player = { src: url, controls: true, preload: video ? "metadata" : "none",
    "aria-describedby": playbackError ? playbackErrorId : undefined,
    ...playerSource.events,
    onCanPlay: () => setPlaybackError(false),
    onError: (event: SyntheticEvent<HTMLMediaElement>) => {
      if (playerSource.recover(event.currentTarget)) return;
      setPlaybackError(event.currentTarget.error?.code === 4 ? "format" : "load");
    } } as const;
  const align = fileBlockAlignment(block.content.align, nested);
  // A video spans the column until its sides are dragged in or out, past the column too (Notion's, seen 2026-11-01 and 2026-11-15).
  const width = video ? draggedWidth ?? (storedWidth && storedWidth !== 100 ? storedWidth : undefined) : storedWidth;
  return <figure ref={playerFigureRef} className={video ? styles.videoBlock : styles.audioBlock} contentEditable={false} data-align={align}
    data-file-source={managed ? "managed" : "external"} data-upload-box={video ? uploadedFrameBox(block, managed, width, align) : undefined}
    data-resizing={draggedWidth !== null ? "true" : undefined} style={frameShareStyle(width, video ? "video" : "audio")}>
    <div ref={frameRef} className={video ? styles.videoFrame : styles.audioFrame} data-playback-error={playbackError ? "true" : undefined}
      data-native-controls={video && !streamed && !webPage && WEBKIT_MEDIA_CONTROLS && !touchScreen() ? "top" : undefined}
      data-pressed={pressed ? "true" : undefined} onPointerLeave={() => setPressed(false)}
      onPointerDown={(event) => { hold.release(); if (event.target === videoRef.current) setPressed(true); }} onKeyDown={hold.release}>
      {streamed ? <iframe className={styles.videoPlayer} src={providerFrameSrc(streamed)} title={blockItemText("media.embeddedVideo")}
        allow={PROVIDER_FRAME_ALLOW} allowFullScreen style={streamedShape ? { aspectRatio: streamedShape } : undefined} />
        : webPage ? <iframe className={styles.videoPlayer} data-web-page src={url} title={blockItemText("media.embeddedVideo")} loading="lazy"
          referrerPolicy={WEB_FRAME_REFERRER} allow="fullscreen" sandbox={webFrameSandbox(url)} />
        : video ? <video ref={videoRef} className={styles.videoPlayer} autoPlay={hold.autoPlay} {...player} playsInline poster={poster || undefined} data-shape={shaped ? undefined : "unknown"}
          onLoadedMetadata={(event) => { playerSource.events.onLoadedMetadata(event); setShaped(event.currentTarget.videoWidth > 0); }}
          onPlay={(event) => { if (hold.stopped(event)) return; playerSource.events.onPlay(); setStarted(true); }} onLoadStart={hold.onLoadStart} />
        : <audio ref={audioRef} className={styles.audioPlayer} {...player} data-app-player={appPlayer ? "" : undefined} />}
      {/* Notion's ring over a video not yet played (seen 2026-10-31); none in Firefox, whose own controls draw their play mark there. */}
      {video && !streamed && !webPage && !started && !playbackError && !FIREFOX ? <button type="button" className={styles.videoPlay}
        aria-label={blockItemText("media.play")} onClick={() => void videoRef.current?.play()} /> : null}
      {/* A video that will not play says so on a plate in its frame, as Notion's (seen 2026-10-31); its help link is left out. */}
      {video && playbackError ? <div id={playbackErrorId} className={styles.videoUnplayable} role="alert">
        <span className={styles.videoUnplayableMark} aria-hidden="true"><VideoUnplayableIcon /><WarningCircleIcon /></span>
        <span>{playbackError === "format"
          ? blockItemText("media.videoFormatUnplayable", { extension: mediaExtension(url, block.content.name) || blockItemText("media.unknownFormat") })
          : blockItemText("media.videoLoadFailed")}</span>
      </div> : null}
      {!video && playbackError ? <div id={playbackErrorId} className={styles.mediaPlaybackError} role="alert">
        {playbackError === "format" ? audioFormatFailure(mediaExtension(url, block.content.name), true)
          : blockItemText("media.audioPlaybackFailed")}</div> : null}
      {video ? <PictureHandlePlace align={align} /> : null}
      {/* Audio has no tools over its player, whose sound and menu they would cover, as Notion's (seen 2026-11-05). */}
      {/* A streaming site's player across the column has no fit to set, as a service's own embed; an uploaded video keeps it (Notion's, seen 2026-11-06). */}
      {video && !adapter.readOnly ? <ImageBlockTools adapter={adapter} block={block} align={align} mode="video"
        canAlign={!streamed || (width !== undefined && width < 100)} /> : null}
      {/* Notion's side pills on a video (seen 2026-11-01): its width alone, the picture keeping its shape. */}
      {video && !adapter.readOnly ? <BlockCornerHandles kind="video" figureRef={playerFigureRef} boxRef={frameRef} grips="sides"
        onSize={(size) => setDraggedWidth(size.width)} onEnd={(size) => {
          setDraggedWidth(null);
          const patch = keptWidthPatch(size, block.content); if (patch) adapter.updateBlock(block, patch);
        }} /> : null}
      {adapter.readOnly ? <div className={styles.mediaActions} data-reader-file-actions><button type="button"
        aria-label={blockItemText(video ? "media.openVideo" : "media.openAudio")} onClick={fileActions.open}>{blockItemText("common.open")}</button></div> : null}
      {fileActions.error ? <span role="alert">{blockItemText(fileActions.blocked ? "file.downloadBlocked" : "file.loadFailed")}</span> : null}
    </div>
  </figure>;
}

/**
 * WebKit (Safari, the Mac app) draws a video's sound at its top right, where
 * the block's tools sit: they go under it there (seen 2026-11-05 in the app).
 * A touch screen's stay at its top over it, 4px in, as Notion's iPhone and iPad
 * (seen 2026-11-13; measured 2026-11-15 at 402: the "⋯" 6px down, 6px in).
 */
const WEBKIT_MEDIA_CONTROLS = typeof navigator !== "undefined"
  && /AppleWebKit/u.test(navigator.userAgent) && !/Chrome|Chromium|Edg\//u.test(navigator.userAgent);

/**
 * Firefox draws its own play mark in the middle of a paused video's controls:
 * Notion draws no ring over one there (its isFirefox, read 2026-11-15 in its
 * code — no Firefox on this Mac to see it; a touch screen has none anyway).
 */
const FIREFOX = typeof navigator !== "undefined" && /\bFirefox\//u.test(navigator.userAgent);

/** A media file's kind, as its name ends: "webm" for clip.webm; a link's name is its address's end. */
function mediaExtension(url: string, fileName: unknown) {
  try {
    const name = typeof fileName === "string" && fileName ? fileName : new URL(url).pathname.split("/").pop() ?? "";
    const dot = name.lastIndexOf(".");
    return dot > 0 ? name.slice(dot + 1).toLowerCase() : "";
  } catch {
    return "";
  }
}

function FileBlock({ adapter, block }: { adapter: NativeEditorGraphAdapter; block: GraphBlock }) {
  const managed = isManagedFileBlock(block);
  const documentApp = useBlockDocumentApp(adapter, block);
  const url = !managed && block.content.source === "external" ? safeMediaUrl(block.content.url) : "";
  // An address kept that opens nothing, as Notion's API keeps "mailto:…", is
  // its name alone, as Notion's file (seen 2026-11-10).
  const stored = !managed && block.content.source === "external" ? storedMediaAddress(block) : "";
  if (managed) {
    return <ManagedFileBlock block={block} session={adapter.session} readOnly={adapter.readOnly} documentApp={documentApp}
      pageId={adapter.pageId} workspaceId={adapter.workspaceId}
      onReplace={() => requestEditorBlockCommand(block.id, "mediaReplace")} />;
  }
  if (url || stored) {
    const name = String(block.content.name || (url && new URL(url).pathname.split("/").filter(Boolean).at(-1)) || stored || blockItemText("file.label"));
    const linkedKind = fileRowKindOf(name, "");
    const LinkedKindIcon = fileKindIcon(linkedKind, FileText);
    return <figure className={styles.fileBlock} contentEditable={false} data-file-source="external">
      <div className={styles.fileCard}>
        {/* Its icon and name alone, as Notion's (seen 2026-11-05); its mark by its kind, as an uploaded one's. */}
        <span className={styles.fileIcon} data-kind={linkedKind ?? undefined}><LinkedKindIcon size="wide" aria-hidden="true" /></span>
        <span className={styles.fileInfo}>
          {url ? <a className={styles.fileName} href={url} target="_blank" rel="noopener noreferrer" data-file-row-link>{name}</a>
            : <span className={styles.fileName}>{name}</span>}
        </span>
        <span className={styles.fileActions}>
          {!adapter.readOnly ? <button type="button" aria-label={blockItemText("file.replaceNamed", { fileName: name })} data-editor-file-action
            onClick={event => requestEditorBlockCommand(block.id, "mediaReplace", undefined, event.currentTarget)}>{blockItemText("common.replace")}</button> : null}
          {url ? <><a href={url} target="_blank" rel="noreferrer" aria-label={blockItemText("file.openNamed", { fileName: name })}
            data-editor-file-action={adapter.readOnly ? undefined : "true"}>{blockItemText("common.open")}</a>
          <a href={url} download aria-label={blockItemText("file.downloadNamed", { fileName: name })}
            data-editor-file-action={adapter.readOnly ? undefined : "true"}>{blockItemText("common.download")}</a></> : null}
        </span>
      </div>
    </figure>;
  }
  if (adapter.readOnly) return <BlockEmptyRow name="file" icon={emptyRowMark("file")} label={emptyText("file").row} />;
  return <MediaUploadForm adapter={adapter} block={block} kind="file" accept="" icon={emptyRowMark("file")}
    initialLink={url} onLink={(value) => {
      const link = givenWebLink(value);
      if (!link) return blockItemText("file.invalidLink");
      const caption = Array.isArray(block.content.caption) && block.content.caption.length > 0 ? { caption: block.content.caption } : {};
      adapter.replaceBlocks([{ ...block, content: {
        source: "external", url: link, name: Array.from(fileNameFromUrl(link)).slice(0, 255).join(""), ...caption,
      } }]);
      return "";
    }} />;
}

export function MediaBlockContent({ adapter, block, nested = false }: { adapter: NativeEditorGraphAdapter; block: GraphBlock; nested?: boolean }) {
  const job = useBlockUploadJob(block.id);
  useTakeFinishedBlockUpload(adapter, block, job);
  if (job && blockUploadRunning(job)) {
    const kind = block.type === "image" || block.type === "video" || block.type === "audio" || block.type === "pdf" ? block.type : "file";
    return <UploadingMedia kind={kind} file={job.file} progress={job.progress} />;
  }
  if (block.type === "image") return <ImageBlock adapter={adapter} block={block} nested={nested} />;
  if (block.type === "video" || block.type === "audio") return <MediaBlock adapter={adapter} block={block} nested={nested} />;
  if (block.type === "bookmark") return <BookmarkBlock adapter={adapter} block={block} />;
  if (block.type === "link_preview") return <LinkPreviewBlock block={block} />;
  if (block.type === "embed") {
    // An app's block.
    if (block.content.service === "app") return <AppEmbedBlock adapter={adapter} block={block} />;
    // An uploaded HTML file, or an empty "HTML" block.
    if (isManagedFileBlock(block)) {
      return normalizedFileContentType(String(block.content.contentType ?? "")) === sandboxedHtmlContentType
        ? <HtmlEmbedFrame adapter={adapter} block={block} nested={nested} />
        : <ManagedEmbedBlock adapter={adapter} block={block} nested={nested} />;
    }
    return block.content.service === "html"
      ? <HtmlEmbedEmpty adapter={adapter} block={block} />
      : <EmbedBlock adapter={adapter} block={block} nested={nested} />;
  }
  if (block.type === "pdf") return <PdfBlock adapter={adapter} block={block} nested={nested} />;
  return <FileBlock adapter={adapter} block={block} />;
}
