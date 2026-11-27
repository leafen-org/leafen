import { type RefObject, useEffect } from "react";

/**
 * After a refused save, the first field the refusal marks takes the focus,
 * whichever press led to the save: the save bar's button, or "저장했습니다"
 * under a recovery code a refused save asked for, which goes on to save
 * (2026-11-22: that save, refused, left the focus on "대신 비밀번호 정하기"
 * beside where the code had been, the marked field out of sight above). A
 * refusal that marks no field leaves the focus where it is. Each refusal
 * counts `turn` up.
 */
export function useRefusedFieldFocus(scope: RefObject<HTMLElement | null>, turn: number) {
  useEffect(() => {
    if (!turn) return;
    const field = scope.current?.querySelector<HTMLElement>("[aria-invalid=true]");
    if (field && document.activeElement !== field) field.focus();
  }, [scope, turn]);
}
