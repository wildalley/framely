import { describe, expect, it } from "vitest";
import {
  formatShortcut,
  heldModifiers,
  isModifierPress,
  mainShortcutKey,
  modifierFromEvent,
  recordShortcut,
  shortcutParts,
  type ShortcutKeyEvent,
} from "./shortcut";

function keyEvent(event: Partial<ShortcutKeyEvent>): ShortcutKeyEvent {
  return {
    code: "",
    key: "",
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    metaKey: false,
    ...event,
  };
}

describe("recordShortcut", () => {
  it("records the Win key as a Super modifier", () => {
    // WebKit reports the lone Win press with the MetaLeft code, and does not
    // always set metaKey on that first event.
    expect(recordShortcut(keyEvent({ code: "MetaLeft", key: "Meta" }))).toEqual({
      status: "pending",
      modifiers: ["Super"],
    });
    expect(
      recordShortcut(keyEvent({ code: "KeyS", key: "s", metaKey: true })),
    ).toEqual({ status: "recorded", shortcut: "Super+S" });
    expect(
      recordShortcut(keyEvent({ code: "KeyA", key: "a", metaKey: true, shiftKey: true })),
    ).toEqual({ status: "recorded", shortcut: "Shift+Super+A" });
  });

  it("orders modifiers so equal combinations compare equal", () => {
    const held = { ctrlKey: true, shiftKey: true, altKey: true, metaKey: true };
    expect(recordShortcut(keyEvent({ code: "KeyQ", key: "q", ...held }))).toEqual({
      status: "recorded",
      shortcut: "Control+Shift+Alt+Super+Q",
    });
  });

  it("names keys the way the native parser expects, ignoring the layout", () => {
    // event.key would be "@" here, which global-hotkey cannot parse.
    expect(recordShortcut(keyEvent({ code: "Digit2", key: "@", ctrlKey: true, shiftKey: true })))
      .toEqual({ status: "recorded", shortcut: "Control+Shift+2" });
    expect(recordShortcut(keyEvent({ code: "PrintScreen", key: "PrintScreen", altKey: true })))
      .toEqual({ status: "recorded", shortcut: "Alt+PrintScreen" });
    expect(recordShortcut(keyEvent({ code: "F5", key: "F5", ctrlKey: true })))
      .toEqual({ status: "recorded", shortcut: "Control+F5" });
    expect(recordShortcut(keyEvent({ code: "Numpad3", key: "3", ctrlKey: true })))
      .toEqual({ status: "recorded", shortcut: "Control+Numpad3" });
  });

  it("keeps listening while only modifiers are held", () => {
    expect(recordShortcut(keyEvent({ code: "ControlLeft", key: "Control", ctrlKey: true })))
      .toEqual({ status: "pending", modifiers: ["Control"] });
    expect(
      recordShortcut(keyEvent({ code: "ShiftRight", key: "Shift", ctrlKey: true, shiftKey: true })),
    ).toEqual({ status: "pending", modifiers: ["Control", "Shift"] });
  });

  it("rejects presses the native registration would refuse", () => {
    expect(recordShortcut(keyEvent({ code: "KeyS", key: "s" }))).toEqual({
      status: "needs-modifier",
    });
    expect(recordShortcut(keyEvent({ code: "Lang1", key: "HangulMode", ctrlKey: true }))).toEqual({
      status: "unsupported",
    });
  });

  it("falls back to the character when a virtual keyboard reports no code", () => {
    expect(recordShortcut(keyEvent({ code: "", key: "k", ctrlKey: true }))).toEqual({
      status: "recorded",
      shortcut: "Control+K",
    });
  });
});

describe("modifier helpers", () => {
  it("recognises every modifier spelling a browser may report", () => {
    expect(isModifierPress(keyEvent({ code: "OSLeft", key: "OS" }))).toBe(true);
    expect(isModifierPress(keyEvent({ code: "SuperR", key: "Super" }))).toBe(true);
    expect(isModifierPress(keyEvent({ code: "AltRight", key: "AltGraph" }))).toBe(true);
    expect(isModifierPress(keyEvent({ code: "KeyS", key: "s" }))).toBe(false);
  });

  it("includes the key being pressed among the held modifiers", () => {
    expect(heldModifiers(keyEvent({ code: "MetaLeft", key: "Meta", ctrlKey: true })))
      .toEqual(["Control", "Super"]);
  });

  it("has no main key for a modifier press", () => {
    expect(mainShortcutKey(keyEvent({ code: "MetaLeft", key: "Meta" }))).toBeNull();
  });
});

describe("display", () => {
  it("labels modifiers the way the keyboard does", () => {
    expect(shortcutParts("Super+Shift+S")).toEqual(["Win", "Shift", "S"]);
    expect(formatShortcut("CommandOrControl+Shift+A")).toBe("Ctrl+Shift+A");
    expect(formatShortcut("Control+PrintScreen")).toBe("Ctrl+PrtSc");
    expect(shortcutParts("")).toEqual([]);
  });
});
