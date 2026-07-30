/**
 * [INPUT]: Named fake Qwen3Guard response cases used by JavaScript runtime tests.
 * [OUTPUT]: Stable structured-output fixtures without real user content.
 * [POS]: Checked-in JavaScript fixture catalog for adapter runtime verification.
 *
 * [PROTOCOL]:
 * 1. Update this header when fixture responsibilities change.
 * 2. Update this folder's .folder.md when this file changes.
 */

export const qwenOutputFixtures = {
  safe: "Safety: Safe",
  controversial: "Safety: Controversial\nCategories: Non-violent Illegal Acts",
  unsafeViolent: "Safety: Unsafe\nCategories: Violent",
  unsafeUnknown: "Safety: Unsafe\nCategories: Newly Invented Unsafe Category",
  malformed: "not a structured classification",
  empty: "",
  unknownLabel: "Safety: Unknown"
};
