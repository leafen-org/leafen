import { useEffect, useState } from "react";
import { blockTypeForPastedAssetUrl } from "./mediaEmbeds";
import styles from "./editorStyles.generated.module.css";

/**
 * What a plain link's embed frames: a picture or a page. Notion's embed goes
 * through its embed service (embed.notion.co), whose own page for a
 * picture's address is the picture over the whole frame — its rule "center /
 * cover no-repeat", cut to the frame from its middle — on no ground of its
 * own, a link that opens the address in a new tab ("base target _blank"),
 * where a page's address is framed as the page (read 2026-11-16 in Chrome at
 * 1440, the service's page for "dummyimage.com/300x200" and its rules; its
 * frame 284 × 200 in a 300 × 216 block, and 84 × 100 for
 * "httpbin.org/image/png"). Framed as a page, a picture whose site refuses
 * frames (X-Frame-Options, as dummyimage.com's) showed Chrome's broken page.
 * That night Notion's copy of the picture did not load (its service's file
 * address answered a page), leaving the frame empty; Leafen's loads the
 * picture from its own address.
 */
export type FramedKind = "picture" | "page";

// Each address's answer, kept while the app runs, so a frame drawn again
// knows at once, and the reads still under way.
const known = new Map<string, FramedKind>();
const reading = new Map<string, Promise<FramedKind>>();
// A read not answered by then frames the address as a page meanwhile; a
// picture that loads later still takes its frame.
const READ_WAIT_MS = 3000;

/** Tries the address once as a picture, as the page would load it. */
function readFramedKind(url: string) {
  let pending = reading.get(url);
  if (!pending) {
    pending = new Promise<FramedKind>((resolve) => {
      const image = new Image();
      image.onload = () => resolve(image.naturalWidth > 0 && image.naturalHeight > 0 ? "picture" : "page");
      image.onerror = () => resolve("page");
      image.referrerPolicy = "no-referrer";
      image.src = url;
    }).then((kind) => {
      known.set(url, kind);
      reading.delete(url);
      return kind;
    });
    reading.set(url, pending);
  }
  return pending;
}

/** A picture's file ending says so without a read; another known file's ending frames it as a page. */
function endingKind(url: string): FramedKind | undefined {
  const kind = blockTypeForPastedAssetUrl(url);
  return kind === "image" ? "picture" : kind ? "page" : undefined;
}

/**
 * What the frame of a plain link's embed shows for `url`: undefined while the
 * address is first read (the frame stays empty, as Notion's while its
 * service answers). An empty `url` is no plain link: its frame is a page.
 */
export function useFramedKind(url: string): FramedKind | undefined {
  const settled: FramedKind | undefined = !url ? "page" : known.get(url) ?? endingKind(url);
  const [read, setRead] = useState<{ url: string; kind: FramedKind } | null>(null);
  useEffect(() => {
    if (settled) return undefined;
    let live = true;
    const wait = window.setTimeout(() => {
      if (live) setRead((current) => current?.url === url ? current : { url, kind: "page" });
    }, READ_WAIT_MS);
    void readFramedKind(url).then((kind) => {
      window.clearTimeout(wait);
      if (live) setRead({ url, kind });
    });
    return () => {
      live = false;
      window.clearTimeout(wait);
    };
  }, [url, settled]);
  return settled ?? (read?.url === url ? read.kind : undefined);
}

/** The picture a plain link's embed frames, as Notion's embed service draws it. */
export function LinkedPictureFrame({ url, label }: { url: string; label: string }) {
  return (
    <a className={styles.linkedPicture} href={url} target="_blank" rel="noopener noreferrer" aria-label={label}
      draggable={false}>
      <img src={url} alt="" loading="lazy" referrerPolicy="no-referrer" draggable={false} />
    </a>
  );
}
