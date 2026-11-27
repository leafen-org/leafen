import { useState } from "react";
import { serverPath } from "@/lib/serverOrigin";
import styles from "./AppRowIcon.module.css";

/**
 * An installed app's own picture in a list, or its first letter when it has
 * none or the picture doesn't load (2026-10-16: Server › Apps showed "O" for
 * an app with ui/icon.svg). On an app's card — the person's /apps and the
 * administrator's "앱 둘러보기" alike — it is a 44px tile rounded 10px
 * (`tile`; 2026-11-19: the browse window's cards showed none).
 */
export function AppRowIcon({ name, iconUrl, className, tile }: { name: string; iconUrl?: string; className?: string; tile?: boolean }) {
  const [broken, setBroken] = useState("");
  const initial = name.slice(0, 1).toUpperCase();
  return (
    <span className={tile ? styles.tile : className} aria-hidden="true">
      {iconUrl && broken !== iconUrl
        ? <img className={styles.image} src={serverPath(iconUrl)} alt="" data-app-icon onError={() => setBroken(iconUrl)} />
        : initial}
    </span>
  );
}
