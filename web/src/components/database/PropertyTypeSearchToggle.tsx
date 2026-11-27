import { Search } from "@/icons/leafen";
import styles from "./PropertyTypeSearchToggle.module.css";

/** A type list's 🔍, which opens its search (PropertyTypeSearchToggle.module.css: no ground of its own while it is open). */
export function PropertyTypeSearchToggle({ label, open, onClick, data }: {
  label: string; open: boolean; onClick(): void; data?: Record<string, string>;
}) {
  return (
    <button type="button" className={styles.toggle} aria-label={label} aria-pressed={open} onClick={onClick} {...data}>
      <Search size="tiny" aria-hidden="true" />
    </button>
  );
}
