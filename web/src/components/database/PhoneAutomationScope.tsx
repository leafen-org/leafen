import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { CheckIcon } from "@/icons/leafen";
import type { DatabaseAutomationScope } from "@/databaseAutomationWire.generated";
import type { DbView } from "@/lib/types";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { ViewIcon, viewDisplayName } from "./databaseViewShared";
import { automationViewFilterCount } from "./databaseAutomationPanelModel";
import styles from "./PhoneAutomationScope.module.css";
import { PhoneSheetSearch } from "@/components/shell/PhoneSheetSearch";

/**
 * Where an automation applies, on a phone, as Notion's "적용할 페이지 위치"
 * (seen 2026-10-24 at 375px): "완료" at the head, a search "보기를 검색하세요",
 * the database ("전체 데이터 소스") with its own icon and each view with its
 * mark ("이 보기에 있는 페이지", or how many filters it holds: "필터 1개"),
 * the chosen one ticked; picking one closes it.
 */
export function PhoneAutomationScopeScreen({ databaseName, databaseIcon, views, scope, onPick, onClose }: {
  databaseName: string;
  databaseIcon?: ReactNode;
  views: DbView[];
  scope: DatabaseAutomationScope;
  onPick(scope: DatabaseAutomationScope): void;
  onClose(): void;
}) {
  const { t } = useTranslation(["databaseView", "common"]);
  const [query, setQuery] = useState("");
  const shown = views.filter((view) => viewDisplayName(view).toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  const tick = (chosen: boolean) => chosen ? <CheckIcon size="regular" aria-hidden="true" /> : undefined;
  const pick = (next: DatabaseAutomationScope) => { onPick(next); onClose(); };
  return (
    <PhoneSheet size="screen" title={t("databaseView:automationScopeTitle")} titleFit="spills" onClose={onClose}
      action={{ label: t("common:actions.done"), onPress: onClose }} data={{ "data-phone-automation-scope": "" }}>
      <div className={sheetStyles.stack}>
        <PhoneSheetGroup>
          <PhoneSheetSearch value={query} placeholder={t("databaseView:automationScopeSearch")}
            aria-label={t("databaseView:automationScopeSearch")} onValueChange={setQuery} />
        </PhoneSheetGroup>
        <PhoneSheetGroup className={styles.group}>
          <PhoneSheetRow icon={databaseIcon} label={databaseName} description={t("databaseView:automationScopeWhole")} more={false}
            value={tick(scope.type === "database")} data={{ "data-phone-automation-scope-choice": "database" }}
            onPress={() => pick({ type: "database" })} />
        </PhoneSheetGroup>
        {shown.length ? (
          <PhoneSheetGroup className={styles.group}>
            {shown.map((view) => (
              <PhoneSheetRow key={view.id} icon={<ViewIcon view={{ type: view.type, config: view.config as Record<string, unknown> | undefined }} size="regular" />}
                label={viewDisplayName(view)}
                description={automationViewFilterCount(view) ? t("databaseView:automationScopeFilters", { count: automationViewFilterCount(view) })
                  : t("databaseView:automationScopeView")} more={false}
                value={tick(scope.type === "view" && scope.viewId === view.id)} data={{ "data-phone-automation-scope-choice": view.id }}
                onPress={() => pick({ type: "view", viewId: view.id })} />
            ))}
          </PhoneSheetGroup>
        ) : null}
      </div>
    </PhoneSheet>
  );
}
