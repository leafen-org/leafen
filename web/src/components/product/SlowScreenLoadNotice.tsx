import { useEffect, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";
import { screenLoadSlow, subscribeSlowScreenLoads } from "@/lib/lazyScreen";
import { useProductToast } from "@/lib/productToast";

/** Says a screen is on its way while its code is slow to come (lib/lazyScreen.ts). */
export function SlowScreenLoadNotice() {
  const { t } = useTranslation("syncStatusBadge");
  const { notify, dismissMessage } = useProductToast();
  const slow = useSyncExternalStore(subscribeSlowScreenLoads, screenLoadSlow, () => false);
  const message = t("status.loadingScreen");
  useEffect(() => {
    if (!slow) return;
    notify(message, "default", undefined, undefined, undefined, undefined, true);
    return () => dismissMessage(message);
  }, [slow, message, notify, dismissMessage]);
  return null;
}
