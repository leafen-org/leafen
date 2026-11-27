import { useEffect, useState } from "react";
import { request } from "@/apiTransport";
import type { Session } from "@/types";

/**
 * An uploaded video's still before it plays, as Notion's (seen 2026-11-07:
 * its server makes a 640 × 360 JPEG of about the first second and gives it
 * as the video's poster), so a video that opens on black isn't a black box.
 * Leafen's server reads no video: the browser holding the video makes the
 * still — the uploader's from the file it sent, else the first editor's to
 * show the video without one — and the server keeps it with the video
 * (product_file_poster.go), read with ?poster=1 on the video's address.
 */

const POSTER_WIDTH = 640;
const POSTER_AT_SECONDS = 1;
const POSTER_QUALITY = 0.82;
const POSTER_WAIT_MS = 20_000;
// WebKit (Safari, the Mac and iPhone apps) draws nothing of a video it hasn't
// shown the first time it is drawn, its frame ready a moment later (seen
// 2026-11-12 in the Mac app: a clear canvas at "seeked", kept as a black
// still; the frame there 100ms on): the frame is drawn again until it comes.
const FRAME_RETRY_MS = 50;
const FRAME_TRIES = 40;

/** Stills made in this tab, by file, shown at once while the server keeps its copy. */
const madeHere = new Map<string, string>();
const making = new Map<string, Promise<string>>();

/** A still from about the first second of a video (half its length when shorter), 640 wide at most. */
export function makeVideoPoster(source: string): Promise<Blob | null> {
  return new Promise((resolve) => {
    const video = document.createElement("video");
    let done = false;
    let retry = 0;
    const finish = (still: Blob | null) => {
      if (done) return;
      done = true;
      window.clearTimeout(timer);
      window.clearTimeout(retry);
      video.removeAttribute("src");
      video.load();
      resolve(still);
    };
    const timer = window.setTimeout(() => finish(null), POSTER_WAIT_MS);
    // A video on another origin — the app's page is the app's own, its server
    // another — comes by CORS, which its server answers (packaged_client.go),
    // so its frame can be read off the canvas.
    if (!source.startsWith("blob:")) video.crossOrigin = "anonymous";
    video.muted = true;
    video.playsInline = true;
    video.preload = "auto";
    video.addEventListener("error", () => finish(null));
    video.addEventListener("loadedmetadata", () => {
      const length = Number.isFinite(video.duration) ? video.duration : 0;
      video.currentTime = length > 0 ? Math.min(POSTER_AT_SECONDS, length / 2) : 0;
    });
    video.addEventListener("seeked", () => {
      const width = Math.min(POSTER_WIDTH, video.videoWidth), height = Math.round(video.videoHeight * (width / Math.max(1, video.videoWidth)));
      if (!width || !height) { finish(null); return; }
      const canvas = document.createElement("canvas");
      canvas.width = width;
      canvas.height = height;
      const context = canvas.getContext("2d");
      if (!context) { finish(null); return; }
      // A drawn frame leaves no pixel clear; a video's own clear middle
      // (one with an alpha channel) is kept as drawn after the last try.
      const draw = (tries: number) => {
        try {
          context.drawImage(video, 0, 0, width, height);
          if (tries < FRAME_TRIES && context.getImageData(width >> 1, height >> 1, 1, 1).data[3] === 0) {
            retry = window.setTimeout(() => draw(tries + 1), FRAME_RETRY_MS);
            return;
          }
          canvas.toBlob((still) => finish(still), "image/jpeg", POSTER_QUALITY);
        } catch {
          // A frame its origin doesn't let this page read makes no still.
          finish(null);
        }
      };
      draw(1);
    });
    video.src = source;
  });
}

async function base64Of(still: Blob) {
  const bytes = new Uint8Array(await still.arrayBuffer());
  let binary = "";
  for (let start = 0; start < bytes.length; start += 0x8000) binary += String.fromCharCode(...bytes.subarray(start, start + 0x8000));
  return btoa(binary);
}

/** Makes the still of a video and has the server keep it; its address here, or "" when none could be made. */
export function keepVideoPoster(session: Session, fileId: string, source: string) {
  const kept = madeHere.get(fileId);
  if (kept) return Promise.resolve(kept);
  const running = making.get(fileId);
  if (running) return running;
  const made = makeVideoPoster(source).then(async (still) => {
    if (!still) return "";
    const shown = URL.createObjectURL(still);
    madeHere.set(fileId, shown);
    await request(`/files/${encodeURIComponent(fileId)}/poster`, { method: "PUT", body: JSON.stringify({ jpeg: await base64Of(still) }) }, session)
      .catch(() => undefined);
    return shown;
  }).finally(() => making.delete(fileId));
  making.set(fileId, made);
  return made;
}

function posterAddress(url: string) {
  return `${url}${url.includes("?") ? "&" : "?"}poster=1`;
}

/**
 * The still a managed video shows before it plays: the one made here, the
 * one the server keeps, or, for an editor, one made now from the video and
 * kept for every reader; "" while there is none.
 */
export function useVideoPoster({ fileId, url, session, editable }: {
  fileId: string; url: string; session: Session | null | undefined; editable: boolean;
}) {
  const [poster, setPoster] = useState(() => madeHere.get(fileId) ?? "");
  useEffect(() => {
    const here = madeHere.get(fileId);
    if (here) { setPoster(here); return; }
    setPoster("");
    if (!fileId || !url) return;
    let live = true, shown = "";
    void (async () => {
      // A video sent from this tab plays from its bytes here: its still comes with it.
      if (!url.startsWith("blob:")) {
        const response = await fetch(posterAddress(url)).catch(() => null);
        if (response?.ok) {
          shown = URL.createObjectURL(await response.blob());
          if (live) setPoster(shown);
          return;
        }
        if (response?.status !== 404) return;
      }
      if (!editable || !session) return;
      const made = await keepVideoPoster(session, fileId, url);
      if (live && made) setPoster(made);
    })();
    return () => {
      live = false;
      if (shown) URL.revokeObjectURL(shown);
    };
  }, [fileId, url, session, editable]);
  return poster;
}
