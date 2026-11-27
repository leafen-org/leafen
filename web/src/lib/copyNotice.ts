import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { copyText } from "@/lib/copyText";
import { useOptionalProductToast } from "@/lib/productToast";

/**
 * A copy button's press: the button stays as it is and a toast says what was
 * copied, as Notion's copy buttons do (seen 2026-11-07: its share menu's
 * "링크 복사" and a code block's copy keep their look while "코드 복사 완료"
 * comes at the bottom). Buttons that swapped their label to "복사됨" changed
 * width and moved what stood beside them. Without a toast stack (a screen
 * drawn before the app), `fallback` is told instead. `writing` is a copy the
 * click already started (Safari copies only what a click starts copying);
 * where it is refused, the text is copied the usual way (copyText.ts).
 */
export function useCopyNotice() {
  const toast = useOptionalProductToast();
  const { t } = useTranslation("common");
  return useCallback(async (
    text: string,
    said?: string,
    fallback?: (message: string, failed: boolean) => void,
    writing?: Promise<unknown>,
  ): Promise<boolean> => {
    const done = writing ? await writing.then(() => true, () => copyText(text)) : await copyText(text);
    const message = done ? (said ?? t("copiedToClipboard")) : t("couldNotCopy");
    if (toast) toast.notify(message, done ? "default" : "error");
    else fallback?.(message, !done);
    return done;
  }, [toast, t]);
}
