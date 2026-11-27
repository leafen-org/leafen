import { type ReactNode, useSyncExternalStore } from "react";
import { I18nProvider } from "react-aria";
import { i18next } from "@/i18n";

// React Aria's menus, tabs, toolbars and lists take their arrow keys' way
// from this locale: in a language read from the right, ArrowLeft opens a
// submenu and moves to the next tab, as the screen is turned over (<html dir>).
function subscribe(changed: () => void) {
  i18next.on("languageChanged", changed);
  return () => { i18next.off("languageChanged", changed); };
}
const language = () => i18next.language || "en";

export function AppLocale({ children }: { children: ReactNode }) {
  return <I18nProvider locale={useSyncExternalStore(subscribe, language, language)}>{children}</I18nProvider>;
}
