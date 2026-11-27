import { useState } from "react";
import { useTranslation } from "react-i18next";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { useSuggestedIdPrefix, useUniqueIdPrefixTaken } from "@/lib/databaseUniqueIdState";
import { Plus } from "../icons";
import styles from "./IdPrefixConfig.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/** The prefixes Notion takes, as the public API checks them (notion_unique_id_prefix.go). */
const ID_PREFIX = /^[A-Za-z][A-Za-z0-9-]{1,9}$/;

/**
 * An ID's prefix, as Notion's editor (seen 2026-11-11): "접두사" on the
 * field's line, the field's "Prefix", the numbers it makes ("PR-1, PR-2,
 * PR-3, …") under it, and a blue button across — "ID 접두사 추가" with a
 * "+" while there is none, "ID 접두사 업데이트" after. It takes capitals as
 * typed; one the rule refuses, or one another data source of the workspace
 * has, shows why in the numbers' place and can't be set; none is no prefix.
 * Changing one it has, to another or to none, asks first. While it has none
 * the field offers one made from the database's title, its text grey as
 * Notion's is until there is a prefix, and the button sets it; the offer is
 * checked only then, one that can't be set saying so as a taken one does.
 */
export function IdPrefixConfig({ dataSourceId, propertyId, prefix, databaseTitle, onKeyDown, onUpdate }: {
  dataSourceId: string; propertyId: string; prefix: string;
  /** The database's title the offer is made from; none while it isn't known. */
  databaseTitle?: string;
  onKeyDown(event: React.KeyboardEvent<HTMLInputElement>): void; onUpdate(prefix: string): void;
}) {
  const { t } = useTranslation("propertyTypeConfig");
  // What the person typed; null while they haven't, the field showing the
  // prefix or the offer.
  const [typed, setTyped] = useState<string | null>(null);
  const [offerRefused, setOfferRefused] = useState(false);
  const suggested = useSuggestedIdPrefix(dataSourceId, propertyId, databaseTitle, !prefix && typed === null);
  const offered = !prefix && typed === null && suggested !== null;
  const draft = typed ?? (offered ? suggested : prefix);
  const refused = !offered && draft !== "" && !ID_PREFIX.test(draft);
  // One another data source of the workspace has is refused as it is typed,
  // in the same place, as Notion's (seen 2026-11-11).
  const taken = useUniqueIdPrefixTaken(dataSourceId, propertyId, draft, !offered && draft !== prefix && draft !== "" && !refused)
    ?? (offerRefused ? draft : null);
  const settable = draft !== prefix && !refused && !taken;
  const numbers = [1, 2, 3].map((number) => draft ? `${draft}-${number}` : String(number)).join(", ");
  const label = prefix ? t("idPrefixUpdate") : t("idPrefixAdd");

  // A prefix it has changes only once asked, as Notion's (seen 2026-11-11):
  // the third-party automations that read its IDs stop, its red "ID 접두사
  // 업데이트" over "취소"; cancelled, the field keeps what was typed and the
  // focus goes back where it was when the question opened (the question's own
  // return): the field after Enter in it, "ID 접두사 업데이트" after a press.
  // Notion's, measured 2026-11-15 in Chrome at 1440 with no script in the
  // page before the action — after "취소", a typed "z" went into the field
  // after Enter and nowhere after the press, the focus on that button.
  async function update() {
    if (!settable) return;
    // An offer the rule refuses is refused once set, as Notion's.
    if (offered && !ID_PREFIX.test(draft)) {
      setOfferRefused(true);
      return;
    }
    if (prefix && !await confirmAction({ message: t("idPrefixChangeConfirm"), confirmLabel: t("idPrefixUpdate"), danger: true })) return;
    onUpdate(draft);
  }
  return (
    <div className={styles.idPrefix} data-id-prefix-editor>
      <label className={styles.idPrefixRow}>
        <span className={styles.idPrefixLabel}>{t("idPrefix")}</span>
        <input className={styles.idPrefixField} value={draft} placeholder={t("idPrefixPlaceholder")} aria-invalid={refused || Boolean(taken)}
          data-unset={prefix ? undefined : ""}
          onKeyDown={(event) => {
            onKeyDown(event);
            // Enter sets it here alone: the menu around doesn't take it, nor
            // the question's button it focuses.
            if (!fieldEnter(event)) return;
            event.preventDefault();
            event.stopPropagation();
            void update();
          }} onChange={(event) => {
            // Capitals as typed, the caret where it was.
            const input = event.target;
            const [start, end] = [input.selectionStart, input.selectionEnd];
            setTyped(input.value.toUpperCase());
            setOfferRefused(false);
            requestAnimationFrame(() => input.setSelectionRange(start, end));
          }} data-id-prefix />
      </label>
      {refused || taken
        ? <p className={styles.idPrefixError} role="alert" data-id-prefix-refused>
          {taken ? t("idPrefixTaken", { prefix: taken }) : t("idPrefixRefused")}</p>
        : <p className={styles.idPrefixHint} data-id-prefix-preview>{t("idPrefixPreview", { numbers })}</p>}
      <div className={styles.idPrefixAction}>
        <button type="button" className={styles.idPrefixButton} disabled={!settable} data-id-prefix-update
          onClick={() => void update()}>
          {prefix ? null : <Plus size="small" aria-hidden="true" />}{label}
        </button>
      </div>
    </div>
  );
}
