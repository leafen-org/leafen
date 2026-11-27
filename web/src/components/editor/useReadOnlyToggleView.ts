import { useEffect, useMemo, useState } from "react";
import type { ContentGraph, GraphBlock } from "@/contentGraph";
import { TOGGLE_BLOCK_TYPES } from "@/lib/blockSemantics";

// Reading a locked page, a shared page or a suggestion must not write its
// graph. All its readers (the caret, children and outline) use this view.
export function useReadOnlyToggleView(graph: ContentGraph, readOnly: boolean, generation: number) {
  const [choice, setChoice] = useState<{ generation: number; collapsed: Map<string, boolean> } | null>(null);

  useEffect(() => setChoice(null), [readOnly, generation]);

  const view = useMemo(() => {
    if (!readOnly || choice?.generation !== generation) return graph;
    return {
      ...graph,
      blocks: graph.blocks.map((block) => {
        const collapsed = choice.collapsed.get(block.id);
        return collapsed === undefined || !TOGGLE_BLOCK_TYPES.has(block.type as never) ? block : {
          ...block, content: { ...block.content, collapsed },
        };
      }),
    };
  }, [graph, readOnly, generation, choice]);

  function change(block: GraphBlock, collapsed: boolean) {
    if (!readOnly) return;
    setChoice((previous) => {
      const choices = new Map(previous?.generation === generation ? previous.collapsed : []);
      choices.set(block.id, collapsed);
      return { generation, collapsed: choices };
    });
  }

  return { graph: view, change };
}
