const MAX_INLINE_EQUATION_CHARACTERS = 1_000;

export function safeInlineEquationValue(value: string | undefined) {
  if (!value || !value.trim() || Array.from(value).length > MAX_INLINE_EQUATION_CHARACTERS) {
    return undefined;
  }
  return value;
}
