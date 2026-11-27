import { Fragment } from "react";
import { Trans, useTranslation } from "react-i18next";
import { BUILT_WITH } from "@/lib/builtWith";
import styles from "./BuiltWithThanks.module.css";

/**
 * One small line under the version in Settings › Server › Updates thanking
 * the tools that help build Leafen, each name linking to its own site in a
 * new tab with no referrer — nothing is asked of them before a press. The
 * names are listed as the screen's language lists things ("A, B and C").
 */
export function BuiltWithThanks() {
  const { t, i18n } = useTranslation("credits");
  let next = 0;
  const names = listParts(BUILT_WITH.map((entry) => entry.name), i18n.language).map((part, index) => {
    if (part.type !== "element") return <Fragment key={index}>{part.value}</Fragment>;
    const entry = BUILT_WITH[next++];
    return (
      <a key={index} href={entry.url} target="_blank" rel="noopener noreferrer" referrerPolicy="no-referrer">{part.value}</a>
    );
  });
  return (
    <span className={styles.thanks} data-built-with="">
      <Trans t={t} i18nKey="credits:builtWithAll" components={{ names: <>{names}</> }} />
    </span>
  );
}

function listParts(names: string[], language: string): { type: "element" | "literal"; value: string }[] {
  try {
    return new Intl.ListFormat(language, { type: "conjunction" }).formatToParts(names);
  } catch {
    return names.flatMap((name, index) => [
      ...(index ? [{ type: "literal" as const, value: ", " }] : []),
      { type: "element" as const, value: name },
    ]);
  }
}
