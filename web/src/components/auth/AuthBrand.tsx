import styles from "../AuthGate.module.css";
import { pageMarkSource } from "@/lib/pageMark";

/** The sign-in card's head: Leafen's mark and name, and the step's line under them. */
export function AuthBrand({ line }: { line: string | null }) {
  return (
    <div className={styles.brand}>
      <img className={styles.mark} src={pageMarkSource()} alt="" aria-hidden="true" />
      <div>
        <h1>Leafen</h1>
        {line ? <p>{line}</p> : null}
      </div>
    </div>
  );
}
