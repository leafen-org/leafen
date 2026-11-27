import type { ReactNode } from "react";
import { styles } from "./topBarStyles";

/** The top bar's current-page crumb when only a title is known; children draw it, its custom emoji among them. */
export function TopBarTitleCrumb({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <span className={styles.crumbWrap}>
      <span className={styles.crumb} data-topbar-current-page>
        <span className={styles.crumbLabel}>{children ?? title}</span>
      </span>
    </span>
  );
}

/**
 * The bar a page shown on its own (a public share) draws before the page is
 * known, as `TopBar` draws it then: the title alone. It stays apart from
 * `TopBar` so the first download carries no editor, menu or comment code.
 */
export function StandaloneTopBarTitle({ title }: { title: string }) {
  return (
    <header className={styles.topbar}>
      <div className={styles.left}><TopBarTitleCrumb title={title} /></div>
      <div className={styles.right} data-topbar-action-rail />
    </header>
  );
}
