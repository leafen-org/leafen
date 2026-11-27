import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useProductToast } from "@/lib/productToast";
import type { PropertyConversionNotice } from "./useContentGraphWrites";

// The toast and labels the editor uses to offer undo after a property type
// change converts cells.
export function usePropertyConversionNotice(): PropertyConversionNotice {
  const { t } = useTranslation("propertyTypeChangeConfirm");
  const { notify } = useProductToast();
  return useMemo(() => ({
    notify,
    labels: {
      changed: t("changedToast"), undo: t("undo"),
      restored: t("restoredToast"), restoreFailed: t("restoreFailedToast"),
    },
  }), [notify, t]);
}
