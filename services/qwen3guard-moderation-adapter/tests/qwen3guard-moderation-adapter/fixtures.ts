/**
 * [INPUT]: Named fake Qwen3Guard response cases used by local contract tests.
 * [OUTPUT]: Stable structured-output fixtures without real user content.
 * [POS]: Shared deterministic fixture catalog for the adapter test boundary.
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
} as const;
