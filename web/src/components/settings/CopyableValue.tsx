import { useTranslation } from "react-i18next";
import { styles } from "@/components/settings/workspaceSettingsOverviewStyles";
import { useCopyNotice } from "@/lib/copyNotice";

/** A value to hand to another system, whole on its lines, with a way to copy it (2026-10-16: cut at the box's end, no copy). */
export function CopyableValue({ value }: { value: string }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const copy = useCopyNotice();
  return (
    <span className={styles.domainRecord}>
      <code>{value}</code>
      <button type="button" className={styles.memberAction} data-copyable-value
        onClick={() => void copy(value)}>
        {t("copy")}
      </button>
    </span>
  );
}
