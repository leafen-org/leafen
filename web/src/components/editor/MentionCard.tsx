import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useRef } from "react";
import { Copy, UserIcon } from "@/icons/leafen";
import { useCopyNotice } from "@/lib/copyNotice";
import { personLabel, unknownPersonLabel } from "@/lib/peopleDirectory";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { placeAround, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { InlineMenuAnchor } from "./blockPickerModel";
import { blockItemText } from "./blockItemLabels";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./editorStyles.generated.module.css";

/** The person mention a card is open for, by what the mention shows. */
export type MentionCardTarget = { kind: "person"; userId: string; text: string; anchor: InlineMenuAnchor };

/**
 * The card a clicked person mention opens: their name and place with a
 * button to copy the name. Escape or a click elsewhere closes it. A page
 * mention opens its page instead, as Notion's does.
 */
export function MentionCard({ adapter, target, onClose }: {
  adapter: NativeEditorGraphAdapter;
  target: MentionCardTarget;
  onClose: () => void;
}) {
  const cardRef = useRef<HTMLDivElement>(null);
  // Under the mention, or over it where only that side has room.
  const place = usePopoverPlacement(cardRef, (size, viewport) => placeAround(
    { ...target.anchor, right: target.anchor.left }, size, viewport, { gap: 8, minHeight: 96 },
  ), [target.anchor], undefined, { kind: "menu", restoreFocus: false, onEscape: onClose, onOutsidePress: onClose, onCaretKeyDown: event => {
    if (event.key !== "Escape") return; event.preventDefault(); event.stopPropagation(); onClose();
  } });
  // The row keeps its words while a toast says the name was copied.
  const copy = useCopyNotice();

  const style = popoverPlacementStyle(place);

  const currentUserId = adapter.session?.user.id;
  usePeopleDirectoryRevision();
  // The directory names the person; one it doesn't know keeps the name the mention shows.
  const known = personLabel(target.userId, currentUserId);
  const inDirectory = Boolean(known) && known !== unknownPersonLabel();
  const label = inDirectory ? known : target.text.replace(/^@/u, "").trim() || known;
  return (
    <div ref={cardRef} className={styles.inlinePersonMenu} style={style} role="dialog" aria-label={blockItemText("person.mention")}
      contentEditable={false} onMouseDown={(event) => event.stopPropagation()}>
      <div className={styles.inlinePersonTop}>
        <PersonAvatar className={styles.inlinePersonAvatar} userId={target.userId} name={label} size={38} />
        <span className={styles.inlinePersonInfo}>
          <span className={styles.inlinePersonName}>{label}</span>
          <span className={styles.inlinePersonMeta}>{blockItemText("person.workspaceMember")}</span>
        </span>
      </div>
      <button type="button" className={styles.inlinePersonAction} onClick={() => void copy(label)}>
        <Copy size="small" aria-hidden="true" />{blockItemText("person.copyName")}
      </button>
      <div className={styles.inlinePersonFoot}><UserIcon size="small" aria-hidden="true" />{blockItemText("person.profile")}</div>
    </div>
  );
}
