import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { TopBar } from "@/components/page/TopBar";
import styles from "@/components/page/PageView.module.css";
import { setDocumentChrome } from "@/lib/documentChrome";
import { useProductPages } from "@/components/product/productPagesContext";
import { AppPlatformMissingNotice } from "./AppPlatformMissing";

/** The apps' pages on a server whose build has none: why, and the way on (AppPlatformMissing.tsx). */
export default function AppsMissingPage({ missing }: { missing: "phone" | "store" }) {
  const { t } = useTranslation("appShell");
  const title = t("route.apps");
  const session = useProductPages()?.session;
  useEffect(() => { setDocumentChrome({ title }); }, [title]);
  return (
    <>
      <TopBar title={title} />
      <div className={styles.missing} data-surface="apps-missing" role="status">
        <h1 className={styles.missingHeading}>{title}</h1>
        <AppPlatformMissingNotice missing={missing} session={session} />
      </div>
    </>
  );
}
