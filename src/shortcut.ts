/**
 * Recording and display of the global capture shortcut.
 *
 * Shortcut strings are spelled the way the native `global-hotkey` parser
 * expects: modifiers `Control`, `Shift`, `Alt`, `Super` followed by exactly one
 * regular key, joined with `+`. `Super` is the Win/Command key.
 */

/** The keyboard fields the recorder needs, so helpers stay testable. */
export interface ShortcutKeyEvent {
  code: string;
  key: string;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
  metaKey: boolean;
}

export type ShortcutRecording =
  /** A complete combination. */
  | { status: "recorded"; shortcut: string }
  /** Only modifiers are down so far; show them and keep listening. */
  | { status: "pending"; modifiers: string[] }
  /** A regular key without any modifier, which cannot be registered globally. */
  | { status: "needs-modifier" }
  /** A key the native registration would reject. */
  | { status: "unsupported" };

/**
 * Key codes accepted verbatim. These match `global-hotkey`'s key names, which
 * is why the recorder reads `event.code` rather than `event.key`: `event.key`
 * depends on the keyboard layout and on which modifiers are held, so Shift+2
 * would arrive as `@` and a Cyrillic layout would produce untranslatable names.
 */
const NAMED_CODES = new Set([
  "Backquote",
  "Backslash",
  "BracketLeft",
  "BracketRight",
  "Comma",
  "Equal",
  "Minus",
  "Period",
  "Quote",
  "Semicolon",
  "Slash",
  "Backspace",
  "CapsLock",
  "Enter",
  "Space",
  "Tab",
  "Delete",
  "End",
  "Home",
  "Insert",
  "PageDown",
  "PageUp",
  "PrintScreen",
  "ScrollLock",
  "Pause",
  "Escape",
  "ArrowDown",
  "ArrowLeft",
  "ArrowRight",
  "ArrowUp",
  "NumLock",
  "NumpadAdd",
  "NumpadDecimal",
  "NumpadDivide",
  "NumpadEnter",
  "NumpadEqual",
  "NumpadMultiply",
  "NumpadSubtract",
]);

/** Modifier order in a recorded shortcut, so equal combinations compare equal. */
export const MODIFIER_ORDER = ["Control", "Shift", "Alt", "Super"] as const;

/**
 * The modifier a key press represents, if any. Reading the code as well as the
 * key matters for the Win key: a lone press reports the `MetaLeft` code, and
 * not every WebKit build also sets `metaKey` on that first event.
 */
export function modifierFromEvent(event: ShortcutKeyEvent): string | null {
  const { code, key } = event;
  if (code.startsWith("Control") || key === "Control") return "Control";
  if (code.startsWith("Shift") || key === "Shift") return "Shift";
  if (code.startsWith("Alt") || key === "Alt" || key === "AltGraph") return "Alt";
  if (
    code.startsWith("Meta") ||
    code.startsWith("Super") ||
    code.startsWith("OS") ||
    code.startsWith("Hyper") ||
    key === "Meta" ||
    key === "OS" ||
    key === "Super" ||
    key === "Hyper"
  ) {
    return "Super";
  }
  return null;
}

/** True when the event is a modifier press rather than a shortcut's main key. */
export function isModifierPress(event: ShortcutKeyEvent): boolean {
  return modifierFromEvent(event) !== null;
}

/** Modifiers held down during an event, in canonical order. */
export function heldModifiers(event: ShortcutKeyEvent): string[] {
  const held = new Set<string>();
  if (event.ctrlKey) held.add("Control");
  if (event.shiftKey) held.add("Shift");
  if (event.altKey) held.add("Alt");
  if (event.metaKey) held.add("Super");
  const pressed = modifierFromEvent(event);
  if (pressed) held.add(pressed);
  return MODIFIER_ORDER.filter((modifier) => held.has(modifier));
}

/** The non-modifier key of an event, named the way the native side expects. */
export function mainShortcutKey(event: ShortcutKeyEvent): string | null {
  if (isModifierPress(event)) return null;
  const letter = /^Key([A-Z])$/.exec(event.code);
  if (letter) return letter[1];
  const digit = /^(?:Digit|Numpad)(\d)$/.exec(event.code);
  if (digit) return event.code.startsWith("Numpad") ? event.code : digit[1];
  if (/^F(?:[1-9]|1\d|2[0-4])$/.test(event.code)) return event.code;
  if (NAMED_CODES.has(event.code)) return event.code;
  // Input methods and virtual keyboards may report an empty code while still
  // giving a printable character.
  if (event.key.length === 1 && /^[a-zA-Z0-9]$/.test(event.key)) return event.key.toUpperCase();
  return null;
}

/** Interpret one key press during recording. */
export function recordShortcut(event: ShortcutKeyEvent): ShortcutRecording {
  const modifiers = heldModifiers(event);
  // A modifier press is an unfinished combination; any other key that cannot be
  // named is one the native registration would reject.
  if (isModifierPress(event)) return { status: "pending", modifiers };
  const key = mainShortcutKey(event);
  if (key === null) return { status: "unsupported" };
  if (modifiers.length === 0) return { status: "needs-modifier" };
  return { status: "recorded", shortcut: [...modifiers, key].join("+") };
}

const DISPLAY_NAMES = new Map([
  ["CommandOrControl", "Ctrl"],
  ["Control", "Ctrl"],
  ["Super", "Win"],
  ["Meta", "Win"],
  ["Command", "Win"],
  ["PrintScreen", "PrtSc"],
  ["Escape", "Esc"],
  ["ArrowUp", "↑"],
  ["ArrowDown", "↓"],
  ["ArrowLeft", "←"],
  ["ArrowRight", "→"],
  ["PageUp", "PgUp"],
  ["PageDown", "PgDn"],
  ["Space", "空格"],
  ["Enter", "回车"],
]);

/** Each key of a shortcut, labelled for display. */
export function shortcutParts(shortcut: string): string[] {
  if (!shortcut) return [];
  return shortcut.split("+").map((part) => DISPLAY_NAMES.get(part) ?? part);
}

/** A shortcut as a single readable string. */
export function formatShortcut(shortcut: string): string {
  return shortcutParts(shortcut).join("+");
}
