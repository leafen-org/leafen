import { type ComponentProps, useEffect, useRef, useState } from "react";
import { copyText } from "@/lib/copyText";
import { useOptionalProductToast } from "@/lib/productToast";
import { IconTipButton } from "./IconTipButton";

/** How long the tip says it was copied, as Notion's (seen 2026-11-15: about a second, then the tip closes). */
const SAID_MS = 1000;

/**
 * An icon button that copies, as Notion's form link buttons (seen 2026-11-15
 * in Chrome, the form's preview and its own page, and in the desktop app's
 * preview): copied, its dark tip says `copied` at once in place of its name
 * for a second and then closes, with no toast. A copy that fails is said in
 * a toast (`failed`); with nothing to copy yet (`text` gives ""), nothing is said.
 */
export function CopyTipButton({ text, copied, failed, ...button }: Omit<ComponentProps<typeof IconTipButton>, "said" | "onClick"> & {
  /** What it copies, read at the press (a form's link once its builder has saved). */
  text(): string | Promise<string>;
  copied: string;
  failed: string;
}) {
  const toast = useOptionalProductToast();
  const [said, setSaid] = useState(false);
  const timer = useRef(0);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return (
    <IconTipButton {...button} said={said ? copied : undefined} onClick={() => void Promise.resolve(text()).then(async (value) => {
      if (!value) return;
      if (!await copyText(value)) { toast?.notify(failed, "error"); return; }
      window.clearTimeout(timer.current);
      setSaid(true);
      timer.current = window.setTimeout(() => setSaid(false), SAID_MS);
    })} />
  );
}
