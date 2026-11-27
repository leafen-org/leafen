import type { BlockContent, BlockType } from "@/lib/types";

export interface PastedBlock {
  type: BlockType;
  content?: BlockContent;
  plainText?: string;
  children?: PastedBlock[];
}

export interface PastedEntry {
  depth: number;
  block: PastedBlock;
}
