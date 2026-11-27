import { useEffect, useState } from "react";
import type { GraphDataSource, GraphProperty } from "@/contentGraph";
import { organizationModule } from "@/lib/organizationModule";
import { OPEN_PROPERTY_ACCESS_EVENT, propertyAccessRestrictable, type OpenPropertyAccessDetail } from "@/lib/propertyAccess";
import type { Session } from "@/types";

/**
 * The organization module's "속성 사용 권한" window for this database's
 * source, opened from one of its columns' menus (TablePropertyMenu); nothing
 * where the module isn't attached.
 */
export function usePropertyAccessDialog({ session, workspaceId, source, properties, onSaved }: {
  session: Session | null;
  workspaceId: string;
  source: GraphDataSource;
  properties: readonly GraphProperty[];
  onSaved: () => void;
}) {
  const [propertyId, setPropertyId] = useState<string | null>(null);
  useEffect(() => {
    const listener = (event: Event) => {
      const detail = (event as CustomEvent<OpenPropertyAccessDetail>).detail;
      if (detail?.dataSourceId === source.id) setPropertyId(detail.propertyId);
    };
    window.addEventListener(OPEN_PROPERTY_ACCESS_EVENT, listener);
    return () => window.removeEventListener(OPEN_PROPERTY_ACCESS_EVENT, listener);
  }, [source.id]);
  const Dialog = organizationModule()?.PropertyAccessDialog;
  if (!Dialog || !session || !propertyId) return null;
  const own = properties.filter((property) => property.dataSourceId === source.id);
  return (
    <Dialog session={session} workspaceId={workspaceId} dataSourceId={source.id} propertyId={propertyId}
      properties={own.filter(propertyAccessRestrictable)} personProperties={own.filter((property) => property.type === "person")}
      onSaved={onSaved} onClose={() => setPropertyId(null)} />
  );
}
