import type { ReactNode } from "react";
import { PhoneSheet, PhoneSheetRow } from "../shell/PhoneSheet";
import { PhoneSheetTick } from "./TablePropertyPhoneSheet";
import styles from "./PhoneSheetChoices.module.css";

/**
 * A select's choices over a phone's property settings, as Notion's "다음과
 * 같이 표시", "날짜 형식" and "숫자 형식" (seen 2026-11-16 at 402pt in iOS
 * Safari): a sheet without a head over the settings, its choices as bands
 * across its whole width straight under the grip, the chosen one ticked
 * where the list ticks one; picking one closes it. A long list one searches
 * on a computer, as "숫자 형식", rises half way first and opens out when pushed
 * up, as Notion's (seen 2026-11-16 twice at 402pt: 45 formats from the
 * screen's middle, none ticked, where "날짜 형식", "소수점 이하 자리" and
 * "다음과 같이 표시" take the whole sheet). A grouping's "정렬" and a status
 * option's "그룹화" are the same whole sheet (PhoneMenuChoice; seen 2026-11-18
 * on a Galaxy A90 at 411 in two tabs, over the sheets' 20% shade).
 */
export function PhoneSheetChoices<Key extends string>({ label, value, choices, long, onPick, onClose, data }: {
  label: string;
  /** A long list, searched on a computer: a half sheet first. */
  long?: boolean;
  /** The chosen one, ticked; none where the list ticks none. */
  value?: Key;
  choices: ReadonlyArray<{ key: Key; label: string; icon?: ReactNode }>;
  onPick(key: Key): void;
  onClose(): void;
  data?: Record<`data-${string}`, string | undefined>;
}) {
  return (
    <PhoneSheet title={label} head={false} size={long ? "half" : "full"} bodyClassName={styles.body} onClose={onClose} data={data}>
      <div className={styles.ground}>
        <div className={styles.list}>
          {choices.map((choice) => (
            <PhoneSheetRow key={choice.key} icon={choice.icon} label={choice.label} more={false}
              value={choice.key === value ? <PhoneSheetTick /> : undefined}
              onPress={() => { onClose(); onPick(choice.key); }}
              data={{ "data-phone-choice": choice.key, "data-chosen": choice.key === value ? "true" : undefined }} />
          ))}
        </div>
      </div>
    </PhoneSheet>
  );
}
