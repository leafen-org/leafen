import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus } from "@/icons/leafen";
import type { PropertyType } from "@/lib/types";
import type { UniqueIdSource } from "@/lib/databaseUniqueIdState";
import { NewPropertyMenu } from "./NewPropertyMenu";
import { RelationPropertyCreationDialog } from "./RelationPropertyCreationDialog";
import type { TableRelationCreation } from "./TablePropertyMenu";
import { ROW_PAGE_ADD_PROPERTY_TYPES } from "./tableAddPropertyModel";
import rowPageStyles from "./DatabaseRowPage.module.css";

// A relation asks which database first; without a way to make one it isn't listed.
const OMIT: readonly PropertyType[] = ["relation"];
const NONE: readonly PropertyType[] = [];

/**
 * "속성 추가" under a database page's properties, as Notion's (seen
 * 2026-10-01, 2026-11-06): Notion's new property in one column over it — a
 * name, the icon tile, the types — and choosing one adds a property of that
 * type named as typed or for its type, whose settings then open at its name
 * (seen 2026-11-01); a relation first asks which database in the same
 * place, then opens its settings too.
 */
export function RowPageAddProperty({ onAdd, onRelationMade, relation, idSource }: {
  /** The data source, whose ID property dims "ID". */
  idSource?: UniqueIdSource;
  /** Makes the property; one that opens its settings answers true, and keeps the keys. */
  onAdd(type: PropertyType, name: string, icon?: string): boolean | void;
  /** A relation made here, whose settings then open at its name, as Notion's (seen 2026-11-07). */
  onRelationMade?(propertyId: string): void;
  /** Where a relation can be made, and its database's property names, which a new one's is told apart from. */
  relation?: { databaseId: string; creation: TableRelationCreation; takenNames?: readonly string[] };
}) {
  const { t } = useTranslation("rowProperties");
  const [menuOpen, setMenuOpen] = useState(false);
  const [relationDraft, setRelationDraft] = useState<string | null>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const omit = relation ? NONE : OMIT;

  function finish(type: PropertyType | null, name = "", icon?: string) {
    setMenuOpen(false);
    if (type === "relation" && relation) {
      setRelationDraft(name);
      return;
    }
    if (type && onAdd(type, name, icon)) return;
    window.requestAnimationFrame(() => buttonRef.current?.focus());
  }

  function endRelation() {
    setRelationDraft(null);
    window.requestAnimationFrame(() => buttonRef.current?.focus());
  }

  return (
    <>
      <button ref={buttonRef} type="button" className={rowPageStyles.rowAddProperty} data-row-add-property aria-haspopup="dialog"
        aria-expanded={menuOpen} onClick={() => setMenuOpen((open) => !open)}>
        <Plus size="small" aria-hidden="true" />
        <span>{t("addProperty")}</span>
      </button>
      {menuOpen ? (
        <NewPropertyMenu anchor={buttonRef} label={t("newPropertyType")} idSource={idSource}
          types={ROW_PAGE_ADD_PROPERTY_TYPES.filter((type) => !omit.includes(type))}
          onCreate={(type, name, icon) => finish(type, name, icon)} onClose={() => setMenuOpen(false)}
          data={{ "data-row-add-property-menu": "" }} />
      ) : null}
      {relationDraft !== null && relation ? (
        <RelationPropertyCreationDialog anchor={buttonRef.current} context={relation.creation.context} databaseId={relation.databaseId}
          initialName={relationDraft} takenNames={relation.takenNames} snapshot={relation.creation.snapshot} onCancel={endRelation}
          onCreated={(property) => {
            setRelationDraft(null);
            if (onRelationMade) onRelationMade(property.id);
            else window.requestAnimationFrame(() => buttonRef.current?.focus());
          }} />
      ) : null}
    </>
  );
}
