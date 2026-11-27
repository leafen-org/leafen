import { useEffect, useState } from "react";
import { LeafenApiError } from "@/apiTransport";
import { ImageBrokenIcon } from "@/icons/leafen";
import { serverOrigin } from "@/lib/serverOrigin";
import type { Session } from "@/types";
import { createUrlMetadataApi } from "@/urlMetadataApi";
import { urlMetadataErrorCodes } from "@/urlMetadataWire.generated";
import { blockItemText } from "./blockItemLabels";
import styles from "./editorStyles.generated.module.css";

// Where an image couldn't be loaded, Notion shows a grey square the image's
// width with a broken picture and what went wrong — "couldn't find" for one
// that is gone, "couldn't load" otherwise — over its help's "자세히
// 알아보기" and the address (seen 2026-10-20, light; 2026-11-12, dark: a
// 404 and an address whose host has no address alike "couldn't find" over
// "example.com(오류 404)", a 500 and a 403 "couldn't load" over the host
// alone). The help's link is left out,
// its line kept.

// The server's own files answer the page's request; another site's answer
// can't be read here, so the server asks it whether anything is there
// (the URL metadata reader's "not found"), as Notion's image proxy does.
function useFailureStatus(url: string, session?: Session | null) {
  const [status, setStatus] = useState(0);
  useEffect(() => {
    setStatus(0);
    if (!url) return;
    let origin = "";
    try {
      origin = new URL(url, window.location.href).origin;
    } catch {
      return;
    }
    let live = true;
    const controller = new AbortController();
    if (origin === window.location.origin || origin === new URL(serverOrigin()).origin) {
      void fetch(url, { method: "HEAD", cache: "no-store" })
        .then((response) => { if (live && !response.ok) setStatus(response.status); })
        .catch(() => undefined);
    } else if (session && /^https?:$/u.test(new URL(url).protocol)) {
      void createUrlMetadataApi().readUrlMetadata(session, url, controller.signal).catch((error: unknown) => {
        if (live && error instanceof LeafenApiError && error.code === urlMetadataErrorCodes.notFound) setStatus(404);
      });
    }
    return () => { live = false; controller.abort(); };
  }, [url, session]);
  return status;
}

/**
 * `refused` is the server's error when it gave no address for its file — one
 * gone or damaged — shown at the server's own address. `session` lets the
 * server ask another site whether its picture is there.
 */
export function ImageLoadFailure({ url, refused = 0, session }: { url: string; refused?: number; session?: Session | null }) {
  const fetched = useFailureStatus(refused ? "" : url, session);
  const status = refused || fetched;
  const gone = status === 404 || status === 410;
  let host = "";
  try {
    const address = new URL(url || serverOrigin(), window.location.href);
    host = address.protocol === "http:" || address.protocol === "https:" ? address.host : "";
  } catch {
    host = "";
  }
  const message = blockItemText(gone ? "image.notFound" : "image.loadFailed");
  return (
    <div className={styles.imageFailure} role="img" aria-label={message} data-image-failed={status || "unknown"}>
      <ImageBrokenIcon aria-hidden="true" />
      <div className={styles.imageFailureText}>
        <div>{message}</div>
        <div className={styles.imageFailureLink} aria-hidden="true" />
      </div>
      {host ? <div className={styles.imageFailureAddress}>{status ? blockItemText("image.failedAt", { host, status }) : host}</div> : null}
    </div>
  );
}
