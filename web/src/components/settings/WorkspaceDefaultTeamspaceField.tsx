import { useEffect, useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { X } from "@/icons/leafen";
import { HoverTip } from "@/components/IconTipButton";
import { type DialogTeamspace } from "@/components/teamspace/teamspaceDialogModel";
import { teamspaceGlyph } from "@/components/teamspace/KeptIconGlyph";
import styles from "./WorkspaceTeamspacesSection.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * "기본 팀스페이스" at the head of the settings teamspaces, as Notion's (seen
 * 2026-11-11): what it is for, a field holding each default teamspace as a
 * chip with its ×, whose words list the open teamspaces the reader owns not
 * in it yet under a "팀스페이스" heading, and "업데이트", dim until the field holds other
 * teamspaces than the saved ones. A workspace may have several default
 * teamspaces and keeps at least one, as Notion's (its code, 2026-11-11): the
 * last chip's × is dim, and "업데이트" stays dim with none, each with a tip
 * above it saying why.
 */
export function WorkspaceDefaultTeamspaceField({ teamspaces, defaultIds, busy, onUpdate }: {
  teamspaces: readonly DialogTeamspace[];
  defaultIds: readonly string[];
  busy: boolean;
  /** Saves the default teamspaces, in the field's order. */
  onUpdate(ids: string[]): Promise<unknown>;
}) {
  const { t } = useTranslation(["workspaceSettingsDialog", "teamspaceDialog"]);
  const titleId = useId(), listId = useId();
  const saved = defaultIds.join("\n");
  const [picked, setPicked] = useState<string[]>(() => [...defaultIds]);
  const [query, setQuery] = useState("");
  const [lit, setLit] = useState(0);
  useEffect(() => { setPicked(saved ? saved.split("\n") : []); }, [saved]);
  // A default teamspace not loaded yet keeps its place without a chip.
  const byId = new Map(teamspaces.map((teamspace) => [teamspace.id, teamspace]));
  const chosen = picked.flatMap((id) => byId.get(id) ?? []);
  const wanted = query.trim().toLocaleLowerCase();
  // Only open teamspaces the reader owns, as Notion's (its code, 2026-11-11:
  // "open" and the reader's teamspace role owner): a default one is open to
  // everybody, and only its owners change that.
  const choices = wanted ? teamspaces.filter((teamspace) => !picked.includes(teamspace.id) && !teamspace.archivedAt
    && teamspace.access === "open" && teamspace.membershipRole === "owner"
    && teamspace.name.toLocaleLowerCase().includes(wanted)) : [];
  const shownLit = Math.min(lit, Math.max(0, choices.length - 1));
  const changed = picked.join("\n") !== saved;
  const updateTip = !changed ? t("workspaceSettingsDialog:teamspacesDefaultUnchanged")
    : picked.length === 0 ? t("workspaceSettingsDialog:teamspacesDefaultAtLeastOne") : "";

  function pick(teamspace: DialogTeamspace) {
    setPicked((current) => current.includes(teamspace.id) ? current : [...current, teamspace.id]);
    setQuery("");
    setLit(0);
  }
  function drop(teamspaceId: string) {
    setPicked((current) => current.filter((id) => id !== teamspaceId));
  }

  return (
    <div className={styles.defaultField} data-default-teamspace>
      <div className={styles.defaultHeading}>
        <strong id={titleId}>{t("workspaceSettingsDialog:teamspacesDefaultTitle")}</strong>
        <span>{t("workspaceSettingsDialog:teamspacesDefaultDescription")}</span>
      </div>
      <div className={styles.defaultLine}>
        <div className={styles.defaultBoxHolder}>
          <div className={styles.defaultBox} data-open={choices.length ? "true" : undefined}>
            {chosen.map((teamspace) => (
              <span key={teamspace.id} className={styles.defaultChip} data-default-teamspace-chip={teamspace.id}>
                <span className={styles.defaultChipIcon} aria-hidden="true">{teamspaceGlyph(teamspace)}</span>
                <span className={styles.defaultChipName}>{teamspace.name}</span>
                {chosen.length === 1 ? (
                  <HoverTip label={t("workspaceSettingsDialog:teamspacesDefaultKeepOne")} placement="top">
                    <button type="button" aria-label={t("workspaceSettingsDialog:teamspacesDefaultRemove", { name: teamspace.name })}
                      disabled><X size="tiny" aria-hidden="true" /></button>
                  </HoverTip>
                ) : <button type="button" aria-label={t("workspaceSettingsDialog:teamspacesDefaultRemove", { name: teamspace.name })}
                  disabled={busy} onClick={() => drop(teamspace.id)}><X size="tiny" aria-hidden="true" /></button>}
              </span>
            ))}
            <input value={query} role="combobox" aria-labelledby={titleId} aria-expanded={choices.length > 0} aria-controls={listId}
              aria-autocomplete="list" aria-activedescendant={choices.length ? `${listId}-${shownLit}` : undefined}
              autoComplete="off" disabled={busy} onChange={(event) => { setQuery(event.target.value); setLit(0); }}
              onKeyDown={(event) => {
                if (imeComposing(event)) return;
                if (event.key === "ArrowDown" && choices.length) { event.preventDefault(); setLit((shownLit + 1) % choices.length); }
                else if (event.key === "ArrowUp" && choices.length) { event.preventDefault(); setLit((shownLit - 1 + choices.length) % choices.length); }
                else if (fieldEnter(event) && choices[shownLit]) { event.preventDefault(); pick(choices[shownLit]); }
                else if (event.key === "Escape" && query) { event.preventDefault(); event.stopPropagation(); setQuery(""); }
                else if (event.key === "Backspace" && !query && chosen.length) drop(chosen[chosen.length - 1].id);
              }} />
          </div>
          {choices.length ? (
            <div className={styles.defaultChoices} role="listbox" id={listId} aria-label={t("teamspaceDialog:title")}>
              <div className={styles.defaultChoicesHeading} role="presentation">{t("teamspaceDialog:title")}</div>
              {choices.map((teamspace, index) => (
                <div key={teamspace.id} id={`${listId}-${index}`} role="option" aria-selected={index === shownLit}
                  className={styles.defaultChoice} data-lit={index === shownLit ? "true" : undefined}
                  onMouseEnter={() => setLit(index)} onMouseDown={(event) => event.preventDefault()} onClick={() => pick(teamspace)}>
                  <span className={styles.defaultChipIcon} aria-hidden="true">{teamspaceGlyph(teamspace)}</span>
                  <span>{teamspace.name}</span>
                </div>
              ))}
            </div>
          ) : null}
        </div>
        {updateTip ? (
          <HoverTip label={updateTip} className={styles.defaultUpdateTip} placement="top">
            <button type="button" className={styles.defaultUpdate} disabled data-default-teamspace-update>
              {t("workspaceSettingsDialog:teamspacesDefaultUpdate")}
            </button>
          </HoverTip>
        ) : <button type="button" className={styles.defaultUpdate} disabled={busy}
          data-default-teamspace-update onClick={() => { void onUpdate(picked); }}>
          {t("workspaceSettingsDialog:teamspacesDefaultUpdate")}
        </button>}
      </div>
    </div>
  );
}
