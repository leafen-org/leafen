import { ArrowLeft } from "@/icons/leafen";
import { usePhoneSheetHead } from "../shell/PhoneSheet";
import styles from "./DatabaseTaskSettings.module.css";
import { PanelHeading } from "./panelHead";

export function DatabaseTaskSettingsHeader({ title, backLabel, onBack }: {
  title: string;
  backLabel?: string;
  onBack?: () => void;
}) {
  // In a phone's sheet the screen names the sheet's own head, its "‹" and
  // its title where every sheet has them, as Notion's (seen 2026-11-11:
  // "속성 편집" and "속성" with the title over "‹", the first group 16 under).
  const inSheet = usePhoneSheetHead({ title, onBack });
  if (inSheet) return null;
  // In the docked view settings it heads the panel (panelHead.tsx).
  return (
    <PanelHeading><div className={styles.header}>
      {onBack ? (
        <button type="button" aria-label={backLabel} onClick={onBack}>
          <ArrowLeft size="small" aria-hidden="true" />
        </button>
      ) : <span />}
      <strong>{title}</strong>
      <span />
    </div></PanelHeading>
  );
}
