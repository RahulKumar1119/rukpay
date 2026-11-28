/**
 * The class constants for the saved-recipients and custom-payment screens. The
 * checks read the class names, because a unit test has no layout: which colors
 * a state can show, whether they reach 7:1, whether the control is 48 pixels
 * high and wide, and that nothing is red.
 */
import { describe, expect, it } from "vitest";

import tailwindConfig from "../../tailwind.config";
import { BUTTON_ACTION, BUTTON_PRIMARY, BUTTON_QUIET, CHOICE_ROW, FIELD, NOTICE_BOX } from "./ui";

const NEW_CONSTANTS = { BUTTON_ACTION, BUTTON_QUIET, FIELD, CHOICE_ROW };

// ---------------------------------------------------------------------------
// Colors. The names the contrast check in smoke.test.ts knows, and the three
// Tailwind slate shades that the shipped buttons already use as hover and
// pressed backgrounds. Their values are Tailwind's own.
// ---------------------------------------------------------------------------

const THEME = (tailwindConfig.theme?.extend?.colors ?? {}) as Record<string, string>;
const PALETTE: Record<string, string> = {
  ...THEME,
  white: "#FFFFFF",
  "slate-50": "#F8FAFC",
  "slate-100": "#F1F5F9",
  "slate-200": "#E2E8F0",
  "slate-700": "#334155",
  "slate-900": "#0F172A",
};
/** The colors a new constant may use for its resting look. */
const PLAIN_COLORS = ["white", "slate-50", "slate-100", "slate-surface", "slate-ink", "amber-surface", "amber-text"];
/** Colors that are allowed only behind `hover:` or `active:`. */
const PRESS_COLORS = ["slate-200", "slate-700", "slate-900"];

function luminance(hex: string): number {
  const channel = (i: number): number => {
    const v = parseInt(hex.slice(i, i + 2), 16) / 255;
    return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(1) + 0.7152 * channel(3) + 0.0722 * channel(5);
}
function contrast(foreground: string, background: string): number {
  const [light, dark] = [luminance(foreground), luminance(background)].sort((a, b) => b - a);
  return ((light as number) + 0.05) / ((dark as number) + 0.05);
}

interface ClassPart {
  /** `hover`, `disabled` and so on. Empty for a plain class. */
  variants: string[];
  /** The utility without its variants, such as `bg-white`. */
  utility: string;
}
const partsOf = (classes: string): ClassPart[] =>
  classes
    .split(/\s+/)
    .filter((name) => name !== "")
    .map((name) => {
      const pieces = name.split(":");
      const utility = pieces.pop() as string;
      return { variants: pieces, utility };
    });

/** Sizes, alignment and widths that begin like a color class but are not one. */
const NOT_A_COLOR = /^(text-(xs|sm|base|lg|xl|[2-9]xl|left|right|center|justify|start|end|wrap|nowrap|balance|pretty|ellipsis|clip)|border-(\d+|dashed|dotted|solid|double|none|[xytrblse]|[xytrblse]-\d+))$/;
const COLOR_UTILITY = /^(text|bg|border|ring|outline|fill|stroke|divide|from|via|to|placeholder|caret|accent|decoration|shadow)-(.+)$/;

/** Every class that names a color, as its utility kind and color token. */
function colorClasses(classes: string): { kind: string; token: string; variants: string[]; name: string }[] {
  return partsOf(classes)
    .filter(({ utility }) => COLOR_UTILITY.test(utility) && !NOT_A_COLOR.test(utility))
    .map(({ variants, utility }) => {
      const match = COLOR_UTILITY.exec(utility) as RegExpExecArray;
      return { kind: match[1] as string, token: match[2] as string, variants, name: [...variants, utility].join(":") };
    });
}

/**
 * The text and background colors of a control in one state. The resting
 * classes come first and each variant in `states` overrides them in turn. The
 * order matches Tailwind's: hover, then pressed, then disabled.
 */
function colorsIn(classes: string, states: string[]): { text?: string; background?: string } {
  const out: { text?: string; background?: string } = {};
  for (const state of [null, ...states]) {
    for (const { variants, kind, token } of colorClasses(classes)) {
      const applies = state === null ? variants.length === 0 : variants.length === 1 && variants[0] === state;
      if (!applies) continue;
      if (kind === "text") out.text = PALETTE[token];
      if (kind === "bg") out.background = PALETTE[token];
    }
  }
  return out;
}

describe("the new class constants", () => {
  it("are not empty and are four different strings", () => {
    const values = Object.values(NEW_CONSTANTS);
    expect(values).toHaveLength(4);
    for (const value of values) expect(value.trim().length).toBeGreaterThan(20);
    expect(new Set(values).size).toBe(4);
  });

  describe("color", () => {
    it.each(Object.entries(NEW_CONSTANTS))("%s uses only colors the contrast check knows", (name, classes) => {
      const used = colorClasses(classes);
      expect(used.length, name).toBeGreaterThan(0);
      for (const { token, variants, name: className } of used) {
        const pressOnly = PRESS_COLORS.includes(token);
        expect(PLAIN_COLORS.includes(token) || pressOnly, `${name}: ${className} is not an allowed color`).toBe(true);
        if (pressOnly) {
          expect(variants.some((variant) => variant === "hover" || variant === "active"), `${name}: ${className} is used outside hover and pressed`).toBe(true);
        }
      }
    });

    it.each(Object.entries(NEW_CONSTANTS))("%s has no red, no pure red and no new palette color", (name, classes) => {
      expect(classes, name).not.toMatch(/\bveto-red\b|\bred\b|\bred-\d|\bbg-rose|\btext-rose|\borange-\d|#?\bff0000\b|dc2626/i);
      for (const { token } of colorClasses(classes)) expect(PALETTE, `${name}: ${token}`).toHaveProperty(token);
    });

    it("keeps the shipped palette names unchanged in the theme", () => {
      expect(THEME).toMatchObject({
        "amber-surface": "#FEF3C7",
        "amber-text": "#78350F",
        "slate-surface": "#1E293B",
        "slate-ink": "#F8FAFC",
      });
    });
  });

  describe("contrast", () => {
    const cases: [string, string, string, string[]][] = [
      ["BUTTON_ACTION", "at rest", BUTTON_ACTION, []],
      ["BUTTON_ACTION", "on hover", BUTTON_ACTION, ["hover"]],
      ["BUTTON_ACTION", "when pressed", BUTTON_ACTION, ["hover", "active"]],
      ["BUTTON_ACTION", "when disabled", BUTTON_ACTION, ["disabled"]],
      ["BUTTON_QUIET", "at rest", BUTTON_QUIET, []],
      ["BUTTON_QUIET", "on hover", BUTTON_QUIET, ["hover"]],
      ["BUTTON_QUIET", "when pressed", BUTTON_QUIET, ["hover", "active"]],
      ["BUTTON_QUIET", "when disabled", BUTTON_QUIET, ["disabled"]],
      ["FIELD", "at rest", FIELD, []],
      ["FIELD", "when read-only", FIELD, ["read-only"]],
      ["FIELD", "when disabled", FIELD, ["disabled"]],
      ["CHOICE_ROW", "at rest", CHOICE_ROW, []],
    ];
    it.each(cases)("%s reaches 7:1 %s", (name, label, classes, states) => {
      const { text, background } = colorsIn(classes, states);
      expect(text, `${name} ${label}: no text color`).toBeDefined();
      expect(background, `${name} ${label}: no background color`).toBeDefined();
      expect(contrast(text as string, background as string), `${name} ${label}`).toBeGreaterThanOrEqual(7);
    });

    it("gives the placeholder of a field 7:1 on the field's background", () => {
      const placeholder = colorClasses(FIELD).find((c) => c.variants.includes("placeholder") && c.kind === "text");
      expect(placeholder, "the field sets no placeholder color").toBeDefined();
      const { background } = colorsIn(FIELD, []);
      expect(contrast(PALETTE[(placeholder as { token: string }).token] as string, background as string)).toBeGreaterThanOrEqual(7);
    });

    it("is not met by reducing opacity: no new constant fades anything", () => {
      for (const [name, classes] of Object.entries(NEW_CONSTANTS)) expect(classes, name).not.toMatch(/opacity/);
    });

    it("finds a low-contrast pair when there is one", () => {
      expect(colorsIn("bg-vault-teal text-white", [])).toEqual({ text: "#FFFFFF", background: "#0F766E" });
      expect(contrast("#FFFFFF", "#0F766E")).toBeLessThan(7);
      expect(colorsIn("bg-white text-slate-surface disabled:text-slate-surface", ["disabled"]).text).toBe("#1E293B");
    });
  });

  describe("the disabled style", () => {
    const DASHED_ON_WHITE = ["disabled:border-2", "disabled:border-dashed", "disabled:border-slate-surface", "disabled:bg-white", "disabled:text-slate-surface"];

    it.each([["BUTTON_ACTION", BUTTON_ACTION], ["BUTTON_QUIET", BUTTON_QUIET]])("%s becomes dark text on white with a dashed border", (_name, classes) => {
      for (const name of DASHED_ON_WHITE) expect(classes.split(/\s+/)).toContain(name);
      expect(classes.split(/\s+/)).toContain("disabled:cursor-not-allowed");
    });

    it("gives a disabled field the same dark text on white, with a dashed border", () => {
      for (const name of ["disabled:border-dashed", "disabled:bg-white", "disabled:text-slate-surface"]) {
        expect(FIELD.split(/\s+/)).toContain(name);
      }
    });

    it("leaves the shipped buttons with their own faded style", () => {
      expect(BUTTON_PRIMARY).toContain("disabled:opacity-60");
      expect(NOTICE_BOX).toContain("bg-amber-surface");
    });
  });

  describe("size, spacing and focus", () => {
    const TEXT_AT_LEAST_16 = /(^|\s)text-(base|lg|xl|[2-9]xl)(\s|$)/;

    it.each([["BUTTON_ACTION", BUTTON_ACTION], ["BUTTON_QUIET", BUTTON_QUIET]])("%s has a 48 by 48 pixel hit area and no double-tap delay", (_name, classes) => {
      const names = classes.split(/\s+/);
      expect(names).toContain("min-h-12");
      expect(names).toContain("min-w-12");
      expect(names).toContain("touch-manipulation");
    });

    it.each([["FIELD", FIELD], ["CHOICE_ROW", CHOICE_ROW]])("%s is at least 48 pixels high", (_name, classes) => {
      expect(classes.split(/\s+/)).toContain("min-h-12");
    });

    it("makes the whole choice row full width, so the row is the target", () => {
      expect(CHOICE_ROW.split(/\s+/)).toEqual(expect.arrayContaining(["flex", "w-full", "cursor-pointer", "items-center", "gap-3"]));
      expect(FIELD.split(/\s+/)).toContain("w-full");
    });

    it.each(Object.entries(NEW_CONSTANTS))("%s writes its text at 16 pixels or more", (_name, classes) => {
      expect(classes).toMatch(TEXT_AT_LEAST_16);
    });

    it.each(Object.entries(NEW_CONSTANTS))("%s keeps the focus ring and animates no more than color", (name, classes) => {
      expect(classes, name).not.toMatch(/outline-none|outline-0|focus:outline-none/);
      expect(classes, name).not.toMatch(/transition-all|\banimate-/);
    });

    it("gives the two buttons a hover and a pressed look", () => {
      for (const classes of [BUTTON_ACTION, BUTTON_QUIET]) {
        expect(classes).toMatch(/(^|\s)hover:bg-/);
        expect(classes).toMatch(/(^|\s)active:bg-/);
      }
    });
  });
});
