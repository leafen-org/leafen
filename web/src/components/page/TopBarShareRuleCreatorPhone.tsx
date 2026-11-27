import { useState } from "react";
import { useTranslation } from "react-i18next";
import { PropertyTypeIcon } from "@/components/database/PropertyTypeIcon";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import { CheckIcon } from "@/icons/leafen";
import type { PublicMutationRole, PublicMutationRuleProperty } from "@/publicMutationWire.generated";
import { soleRuleProperty } from "./topBarShareModel";
import { TopBarSharePermissionPicker } from "./TopBarSharePermissionPicker";
import css from "./TopBarShareRuleCreatorPhone.module.css";

type RuleRole = "view" | "comment" | "edit";

/**
 * A phone's "새 규칙 추가" in a database's share "고급", as Notion's (read
 * 2026-11-18 on a Galaxy A90's Chrome at 411 in a Business database, w1949):
 * the window a computer opens laid in a whole sheet with no head under the
 * grip, 8px in — "사용자 속성 선택" and the person properties, a ✓ at the one
 * chosen, then under a line the words, the role ("편집 허용 ⌄", which opens
 * "사용 권한 변경" over it) and "규칙 생성하기", each in its touch size
 * (TopBarShareRuleCreatorPhone.module.css).
 */
export function PhoneRuleCreator({ properties, roles, onClose, onCreate }: {
  properties: PublicMutationRuleProperty[]; roles: PublicMutationRole[];
  onClose(): void; onCreate(property: PublicMutationRuleProperty, role: RuleRole): void;
}) {
  const { t } = useTranslation("topBar");
  const [chosen, setChosen] = useState<PublicMutationRuleProperty | null>(() => soleRuleProperty(properties));
  // Lit only once pressed: the one it starts on is ticked, not lit (Notion's, 2026-11-18 on the A90).
  const [pressed, setPressed] = useState(false);
  const [role, setRole] = useState<RuleRole>("edit");
  return <PhoneSheet title={null} head={false} label={t("addRule")} bodyClassName={css.body} onClose={onClose}
    data={{ "data-share-page-rule-creator": "" }}>
    <div className={css.window}>
      <div className={css.properties} role="listbox" aria-label={t("chooseUserProperty")}>
        <div className={css.heading}>{t("chooseUserProperty")}</div>
        {properties.map(property => <button key={`${property.dataSourceId}:${property.propertyId}`} type="button" role="option"
          className={css.property} aria-selected={chosen === property} data-lit={pressed && chosen === property ? "true" : undefined}
          data-share-page-rule-property={property.type} onClick={() => { setChosen(property); setPressed(true); }}>
          <span className={css.mark} aria-hidden="true"><PropertyTypeIcon type={property.type} size="wide" /></span>
          <span className={css.name}>{property.name || t("ruleCreator")}</span>
          {chosen === property ? <CheckIcon size="page" className={css.check} aria-hidden="true" /> : null}
        </button>)}
      </div>
      <div className={css.grant}>
        <p className={css.text}>{t("ruleGrantText")}</p>
        <div className={css.role}>
          <TopBarSharePermissionPicker value={role} disabled={false} invite layout="full" roles={roles} onChange={next => setRole(next as RuleRole)} />
        </div>
        <button type="button" className={css.create} disabled={!chosen} data-share-page-rule-create
          onClick={() => { if (chosen) onCreate(chosen, role); }}>
          {t("createRule")}
        </button>
      </div>
    </div>
  </PhoneSheet>;
}
