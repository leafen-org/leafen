import type { ProductStatusSink } from "@/lib/productStatus";
import type { Dispatch, SetStateAction } from "react";
import { i18next } from "@/i18n";
import { api } from "@/api";
import {
  createGraphProperty,
  type ContentGraph,
  type GraphDataSource,
  type GraphOperation,
  type GraphProperty,
  type GraphValueType,
  type GraphView,
} from "@/contentGraph";
import { createNativeFormService } from "@/lib/nativeFormService";
import type { Session } from "@/types";

type NativeFormProductAdapterOptions = {
  graph: ContentGraph;
  /** The graph as drawn, which a new property is placed among (pageGraphStore drawn). */
  readGraph?: () => ContentGraph;
  pageId: string | null;
  persistGraphOperations: (
    pageId: string,
    operations: GraphOperation[],
  ) => Promise<boolean>;
  session: Session;
  setGraph: Dispatch<SetStateAction<ContentGraph>>;
  setMessage: ProductStatusSink;
};

// Properties shown before the server has them (addGraphProperty): a form
// saved meanwhile names its new questions' properties, so it goes once they
// are written, whichever adapter made them (one is made again as the graph's
// properties change).
const propertiesBeingSaved = new Set<Promise<boolean>>();

/**
 * Waits for the properties made here to be written, for a setting that names
 * them — a form's questions, a task database's roles (2026-11-13: turning a
 * database into a task database named its new 담당자 and 마감일 before the
 * server had them, and was refused). Says whether any were still being saved.
 */
export async function afterMadePropertiesSaved(): Promise<boolean> {
  if (propertiesBeingSaved.size === 0) return false;
  await Promise.allSettled(propertiesBeingSaved);
  return true;
}

export function createNativeFormProductAdapter({
  graph,
  readGraph,
  pageId,
  persistGraphOperations,
  session,
  setGraph,
  setMessage,
}: NativeFormProductAdapterOptions) {
  const service = createNativeFormService(api, () => session);
  const formService: typeof service = {
    ...service,
    configure: async (input) => {
      await afterMadePropertiesSaved();
      return service.configure(input);
    },
  };
  // Properties made here that this graph doesn't hold yet, so one made right
  // after another comes after it rather than in the same place.
  const made: GraphProperty[] = [];

  async function addGraphProperty(
    source: GraphDataSource,
    type: GraphValueType,
    name: string,
    config: Record<string, unknown> = {},
  ): Promise<GraphProperty | null> {
		const authorityPageId = source.pageId ?? pageId;
		if (!authorityPageId) return null;
    // Placed after every property known here — the page's, the ones drawn
    // and those made here — so it never takes the place of one made just
    // before it another way (a column's copy), which the page's may not hold yet.
    const known = new Map([...graph.properties, ...(readGraph?.().properties ?? []), ...made]
      .filter((property) => property.dataSourceId === source.id).map((property) => [property.id, property]));
    const created = createGraphProperty(
      [...known.values()],
      source.id,
      type,
      name,
      config,
    );
    // Shown at once and saved behind it, as a setting is (user decision
    // 2026-11-12: a press answers on the next frame); one the server refuses
    // goes as the page is read again. Shown once saved, a form's new question
    // took 208 ms on a NAS's 100 ms round trip (2026-11-13).
    const saved = persistGraphOperations(authorityPageId, created.operations);
    propertiesBeingSaved.add(saved);
    void saved.then((written) => {
      if (written) setMessage(i18next.t("databaseView:status.saved"));
    }, () => undefined).finally(() => propertiesBeingSaved.delete(saved));
    made.push(created.property);
    setGraph((current) => ({
      ...current,
      properties: [...current.properties, created.property],
    }));
    return created.property;
  }

  function applyConfiguredFormView(view: GraphView) {
    setGraph((current) => ({
      ...current,
      views: current.views.map((item) => item.id === view.id ? view : item),
    }));
  }

  return { addGraphProperty, applyConfiguredFormView, formService };
}
