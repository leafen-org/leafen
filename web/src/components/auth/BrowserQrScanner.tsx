import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { X } from "@/components/icons";
import styles from "./BrowserQrScanner.module.css";

/**
 * A browser's camera reading one QR code, for the sign-in screen's "QR 찍기"
 * where the phone app's own scanner isn't there: the camera is asked for
 * only now, the frames go to the browser's BarcodeDetector, or to jsQR
 * (loaded only then) where it has none. A page that isn't https can't open
 * the camera; that and a refused or missing camera end it with why, and the
 * close button ends it with nothing.
 */
export type BrowserQrScanEnd = "cancelled" | "denied" | "insecure" | "unavailable";

type Detector = { detect(source: CanvasImageSource): Promise<Array<{ rawValue?: string }>> };
type DetectorClass = {
  new (options: { formats: string[] }): Detector;
  getSupportedFormats?: () => Promise<string[]>;
};

const FRAME_MS = 200;
const DECODE_WIDTH = 640;

async function qrReader(): Promise<(video: HTMLVideoElement, canvas: HTMLCanvasElement) => Promise<string>> {
  const Native = (window as unknown as { BarcodeDetector?: DetectorClass }).BarcodeDetector;
  if (Native && (!Native.getSupportedFormats || (await Native.getSupportedFormats().catch((): string[] => [])).includes("qr_code"))) {
    const detector = new Native({ formats: ["qr_code"] });
    return async (video) => (await detector.detect(video))[0]?.rawValue ?? "";
  }
  const { default: jsQR } = await import("jsqr");
  return async (video, canvas) => {
    const scale = Math.min(1, DECODE_WIDTH / (video.videoWidth || DECODE_WIDTH));
    const width = Math.max(1, Math.round(video.videoWidth * scale));
    const height = Math.max(1, Math.round(video.videoHeight * scale));
    canvas.width = width;
    canvas.height = height;
    const context = canvas.getContext("2d", { willReadFrequently: true });
    if (!context) return "";
    context.drawImage(video, 0, 0, width, height);
    return jsQR(context.getImageData(0, 0, width, height).data, width, height, { inversionAttempts: "attemptBoth" })?.data ?? "";
  };
}

export function BrowserQrScanner({ onScanned, onEnd }: {
  onScanned(text: string): void;
  onEnd(reason: BrowserQrScanEnd): void;
}) {
  const { t } = useTranslation(["authGate", "common"]);
  const video = useRef<HTMLVideoElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const handlers = useRef({ onScanned, onEnd });
  handlers.current = { onScanned, onEnd };

  useEffect(() => {
    let live = true;
    let stream: MediaStream | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const stop = () => {
      live = false;
      if (timer) clearTimeout(timer);
      stream?.getTracks().forEach((track) => track.stop());
    };
    void (async () => {
      if (!window.isSecureContext) return handlers.current.onEnd("insecure");
      if (!navigator.mediaDevices?.getUserMedia) return handlers.current.onEnd("unavailable");
      try {
        stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: { ideal: "environment" } }, audio: false });
      } catch (error) {
        const name = (error as { name?: string } | null)?.name;
        if (live) handlers.current.onEnd(name === "NotAllowedError" || name === "SecurityError" ? "denied" : "unavailable");
        return;
      }
      if (!live || !video.current || !canvas.current) return stop();
      video.current.srcObject = stream;
      await video.current.play().catch(() => undefined);
      const read = await qrReader().catch(() => null);
      if (!read) {
        stop();
        handlers.current.onEnd("unavailable");
        return;
      }
      const look = async () => {
        if (!live || !video.current || !canvas.current) return;
        const text = video.current.readyState >= 2 ? await read(video.current, canvas.current).catch(() => "") : "";
        if (!live) return;
        if (text) {
          stop();
          handlers.current.onScanned(text);
          return;
        }
        timer = setTimeout(() => void look(), FRAME_MS);
      };
      void look();
    })();
    return stop;
  }, []);

  return (
    <div className={styles.scanner} role="dialog" aria-modal="true" aria-label={t("authGate:scanCode")} data-browser-qr-scanner>
      <video ref={video} className={styles.video} muted playsInline aria-hidden="true" />
      <canvas ref={canvas} hidden />
      <div className={styles.frame} aria-hidden="true" />
      <button type="button" className={styles.close} onClick={() => handlers.current.onEnd("cancelled")} aria-label={t("common:actions.close")}>
        <X size="regular" aria-hidden="true" />
      </button>
      <p className={styles.hint}>{t("authGate:scanHint")}</p>
    </div>
  );
}
