import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DbProperty } from "@/lib/types";
import type { FormulaScopeName } from "@/components/database/formulaEditorModel";
import { MentionMenu } from "@/components/editor/MentionMenu";
import { blockItemLabels } from "@/components/editor/blockItemLabels";
import type { BlockPickerSnapshot, InlineMenuAnchor, MentionItem } from "@/components/editor/blockPickerModel";

const RUN_NAMES = new Set(["executionActor", "pageCreator", "executionTime", "executionDate", "triggerPage"]);

// Where no page's mentions are at hand, the menu offers the run's alone.
const NO_SNAPSHOT: BlockPickerSnapshot = {
  currentUserId: "", organizationProfiles: [], pagePresencePeers: [], pagesById: {},
  searchOrganizationPeople: async () => ({ people: [] }) as never,
};

/**
 * An automation message's "@" menu, as Notion's message box lists it (seen
 * 2026-10-23): "사용자 지정" the variables earlier actions define, "속성
 * 출처" the trigger page, whose "›" lists its properties in the menu's place
 * ("‹ 페이지 실행의 속성"), then dates, people and pages, each led by what the
 * run gives — when it ran and on what day, who ran it and the page's maker,
 * the trigger page — before the menu's own. Each of the run's goes in as a
 * chip the server reads as it runs (interactive_automation_message.go).
 */
export function AutomationMentionMenu({ menu, names, properties, snapshot }: {
  menu: { anchor: InlineMenuAnchor; query: string; pick(item: MentionItem): void; close(): void };
  /** The names the message's formulas run with (automationFormulaNames). */
  names: readonly FormulaScopeName[];
  /** The trigger page's properties and created and edited fields. */
  properties: readonly DbProperty[];
  snapshot: BlockPickerSnapshot | null;
}) {
  const { t } = useTranslation("databaseView");
  const [propertiesOpen, setPropertiesOpen] = useState(false);
  const query = menu.query.trim().toLowerCase();
  const ownPage = names.find((name) => name.name === "triggerPage" && name.fields);
  const items = useMemo((): MentionItem[] => {
    const shown = (label: string) => !query || label.toLowerCase().includes(query);
    if (propertiesOpen) {
      // A page button's page lists its own fields in its own words ("‹ 이 페이지의 속성", "생성자:"), as Notion's (seen 2026-10-25).
      const fields = ownPage?.fields ?? properties.map((property) => ({ property, label: property.name }));
      return fields.filter(({ label }) => shown(label)).map(({ property, label }) => ({
        kind: "automation", id: `property:${property.id}`, label, description: "", icon: "", groupLabel: "",
        token: `triggerPage.prop(${JSON.stringify(property.name)})`, tokenType: property.type,
        chip: t(ownPage ? "thisPageProperty" : "triggerPageProperty", { name: label }),
      }));
    }
    const labels = blockItemLabels();
    const item = (name: FormulaScopeName, groupLabel: string, opens = false): MentionItem => ({
      kind: "automation", id: `${opens ? "source" : groupLabel}:${name.name}`, label: name.label, description: name.description,
      icon: "", groupLabel, token: name.name, tokenType: name.type, ...(opens ? { opens } : {}),
    });
    const named = (...keys: string[]) => keys.flatMap((key) => names.filter((name) => name.name === key));
    const page = named("triggerPage");
    return [
      ...names.filter((name) => !RUN_NAMES.has(name.name)).map((name) => item(name, t("mentionGroups.custom"))),
      ...page.map((name) => item(name, t("mentionGroups.source"), true)),
      ...named("executionTime", "executionDate").map((name) => item(name, labels.groupDate)),
      ...named("executionActor", "pageCreator").map((name) => item(name, labels.groupPeople)),
      ...page.map((name) => item(name, labels.groupLinkToPage)),
    ].filter((candidate) => shown(candidate.label));
  }, [names, ownPage, properties, propertiesOpen, query, t]);
  return (
    <MentionMenu anchor={menu.anchor} query={menu.query} snapshot={snapshot ?? NO_SNAPSHOT} onClose={menu.close}
      onPick={(picked) => (picked.kind === "automation" && picked.opens ? setPropertiesOpen(true) : menu.pick(picked))}
      automation={{ items, alone: propertiesOpen ? { label: t(ownPage ? "thisPageProperties" : "triggerPageProperties"), onBack: () => setPropertiesOpen(false) } : undefined }} />
  );
}
