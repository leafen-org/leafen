import { useEffect, useState } from "react";
import { withLoadDeadline } from "@/lib/lazyScreen";

/** A QR picture of an address, for a phone to open; "" until drawn. */
export function useQRCode(value: string, width = 168): string {
  const [picture, setPicture] = useState("");
  useEffect(() => {
    let current = true;
    setPicture("");
    if (!value) return undefined;
    void withLoadDeadline(import("qrcode")).then(({ default: QRCode }) => QRCode.toDataURL(value, { margin: 1, width }))
      .then((made) => { if (current) setPicture(made); }, () => undefined);
    return () => { current = false; };
  }, [value, width]);
  return picture;
}
