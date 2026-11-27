import { SwitchMark } from "@/components/Switch";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import { useShownChanges } from "@/lib/shownChanges";
import { updatePeopleProfileSettings, usePeopleProfileSettings, type PeopleProfileSettings } from "@/lib/peopleProfiles";
import type { Session, Workspace } from "@/types";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";

const SWITCHES = [
  { key: "directory", label: "peopleDirectoryField", description: "peopleDirectoryDescription" },
  { key: "activity", label: "peopleActivityField", description: "peopleActivityDescription" },
  { key: "hoverCards", label: "peopleHoverCardsField", description: "peopleHoverCardsDescription" },
] as const;

/**
 * "사람" under 일반, as Notion's (seen 2026-10-12): the owner's switches for
 * the people list and profile pages, the recent activity a profile shows and
 * the card a name shows, each on at first. A switch turns as it is pressed
 * and is put back with why if the server refuses (lib/shownChanges.ts).
 */
export function WorkspacePeopleProfileSettings({ session, workspace }: { session: Session; workspace: Workspace }) {
  const { t } = useTranslation("workspaceSettingsDialog");
  const profiles = usePeopleProfileSettings();
  // The switches are the workspace's people read's: none until it is in (a
  // read that failed showed them all on, 2026-11-20); their words stay.
  const known = profiles?.workspaceId === workspace.id;
  const settings: PeopleProfileSettings = profiles?.workspaceId === workspace.id
    ? profiles.settings : { directory: true, activity: true, hoverCards: true };
  const pending = useShownChanges<PeopleProfileSettings>();
  const shown = { ...settings, ...pending.shown };
  const [error, setError] = useState("");

  function change(key: keyof PeopleProfileSettings) {
    const next = { [key]: !shown[key] };
    setError("");
    void pending.change(next, () => updatePeopleProfileSettings(session, workspace.id, next))
      .catch((failure: unknown) => setError(settingsErrorMessage(failure, t("errUpdatePeopleProfiles"))));
  }

  return (
    <div data-workspace-people-profiles>
      {SWITCHES.map(({ key, label, description }) => (
        <div key={key} className={settingsStyles.securityRow} data-people-profile-switch={key}>
          <div className={settingsStyles.securityInstruction}>
            <strong>{t(label)}</strong>
            <span>{t(description)}</span>
          </div>
          {known ? <button type="button" role="switch" aria-checked={shown[key]} aria-label={t(label)}
            data-waiting-note={pending.note(key)} onClick={() => change(key)}>
            <SwitchMark on={shown[key]} />
          </button> : null}
        </div>
      ))}
      {error ? <div className={settingsStyles.errorText} role="alert">{error}</div> : null}
    </div>
  );
}
