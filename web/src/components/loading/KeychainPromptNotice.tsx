"use client";

import { useEffect, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import { ToastStack } from "@/components/ToastStack";
import { followKeychainPrompt, keychainPromptShowing } from "@/lib/keychainPrompt";
import { appToastStack } from "@/lib/productToast";

const noServerSnapshot = () => false;
const leaveAsIs = () => undefined;

/**
 * While a keychain call waits on macOS's prompt (lib/keychainPrompt.ts), one
 * line with a turning ring says so, where the app's notices show, until the
 * call answers: through the app's own stack once the app is drawn, and in a
 * stack of its own before (the start, the first screens).
 */
export function KeychainPromptNotice() {
  const { t } = useTranslation("app");
  const showing = useSyncExternalStore(followKeychainPrompt, keychainPromptShowing, noServerSnapshot);
  const message = t("app:keychainAsking");
  const stack = showing ? appToastStack() : null;
  useEffect(() => {
    if (!stack) return undefined;
    stack.notify(message, "default", undefined, "progress", undefined, undefined, true);
    return () => stack.dismissMessage(message);
  }, [stack, message]);
  if (!showing || stack) return null;
  return <ToastStack toasts={[{ id: "keychain-prompt", icon: "progress", message }]} dismissToast={leaveAsIs} />;
}
