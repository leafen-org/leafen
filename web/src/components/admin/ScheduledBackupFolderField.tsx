import { useTranslation } from "react-i18next";
import { chooseServerFolder, canChooseServerFolder, deviceFolderName, pickedFoldersOnly } from "../../lib/deviceFolder";
import styles from "./AdministratorSettingsPage.module.css";
import { SettingField, SettingRow } from "./AdministratorSettingRows";

/**
 * The backup folder's field, as where the server runs takes one: on a
 * computer a path typed (with "폴더 선택…" beside it in the desktop app on
 * its own server); on a phone's own server a folder picked with the phone's
 * picker, shown by its name with the button that picks it, as there is no
 * path to type there (2026-11-21: the phone showed a Mac path's example and
 * a button that did nothing). Another server's folder is typed as it names
 * it.
 */
export function ScheduledBackupFolderField({ folder, placeholder, help, disabled, busy, choosable, invalid, onChange }: {
  folder: string;
  placeholder: string;
  help: string;
  disabled: boolean;
  busy: boolean;
  /** Whether picking applies here (not when the environment sets the place). */
  choosable: boolean;
  /** The refused field's marks, while the status line refuses it. */
  invalid: Record<string, unknown>;
  onChange: (folder: string) => void;
}) {
  const { t } = useTranslation("serverConsole");

  async function pick() {
    const chosen = await chooseServerFolder(t("schedule.chooseTitle")).catch(() => null);
    if (chosen) onChange(chosen);
  }

  const picked = deviceFolderName(folder);
  if (pickedFoldersOnly() || picked !== null) {
    return (
      <>
        <SettingRow title={t("schedule.folder")} description={picked || folder || t("schedule.noFolderPicked")}
          data-scheduled-backup-device-folder="">
          {choosable && pickedFoldersOnly() ? (
            <button type="button" className={styles.secondaryButton} onClick={() => void pick()} disabled={disabled}
              data-scheduled-backup-choose {...invalid}>
              {t("schedule.choose")}
            </button>
          ) : <span />}
        </SettingRow>
        <p className={styles.settingNote}>{help}</p>
      </>
    );
  }
  return (
    <>
      <SettingField title={t("schedule.folder")} description={help}>
        <input value={folder} placeholder={placeholder} disabled={disabled}
          maxLength={4096} spellCheck={false} onChange={(event) => onChange(event.target.value)}
          data-scheduled-backup-folder {...invalid} />
      </SettingField>
      {choosable && canChooseServerFolder() ? (
        <div>
          <button type="button" className={styles.secondaryButton} onClick={() => void pick()} disabled={busy}
            data-scheduled-backup-choose>
            {t("schedule.choose")}
          </button>
        </div>
      ) : null}
    </>
  );
}
