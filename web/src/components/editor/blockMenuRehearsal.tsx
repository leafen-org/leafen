import { rehearseAfterStart } from "@/lib/pressRehearsal";
import { touchScreen } from "@/lib/screenKind";
import { BlockActionMenu } from "./BlockActionMenu";
import { BlockMenuFooter } from "./BlockMenuFooter";

const none = () => {};

/**
 * The block menu a line's ⋮⋮, ⌘/ or right click opens, drawn once out of
 * sight after the page's start (pressRehearsal.tsx): a line's rows, with its
 * foot, beside a handle, as a first press draws it. Asked for by a page's
 * editor (Editor.tsx), once for the page's life. A touch screen's menu is a
 * sheet that opens from its tools, not drawn here.
 */
export function rehearseBlockMenu() {
  if (touchScreen()) return;
  rehearseAfterStart("block-menu", () => (
    <BlockActionMenu activeColor="default" activeType="paragraph" selectionCount={1}
      anchor={{ x: 40, y: 40, handle: { top: 40, bottom: 64, left: 40, right: 58 } }}
      footer={<BlockMenuFooter session={null} pageId="" blockIds={[]} texts={["가나다 abc"]} />}
      onClose={none} onColor={none} onComment={none} onSuggest={none} onPresent={none} onCopyLink={none}
      onDelete={none} onDuplicate={none} onMove={none} toggleCaption={none} turnInto={none} />
  ));
}
