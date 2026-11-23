/**
 * Smoke checks for rules that cover the whole project rather than one feature:
 * what the source may contain, what the running app may write, whether the
 * colors are readable, whether every control is big enough, and which
 * libraries the project may depend on.
 */
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import tailwindConfig from "../../tailwind.config";
import {
  guardianPhone,
  HIGH_BUTTON,
  locked,
  presetButton,
  renderApp,
  SAFE_BUTTON,
  userPhone,
  view,
} from "./appHarness";

// ---------------------------------------------------------------------------
// The files under test, read as text. Vite reads them at build time, so no
// Node file API is needed.
// ---------------------------------------------------------------------------

/** Every production source file: `src/` outside `src/test/` and without the test files. */
const production = import.meta.glob<string>(["../**/*.{ts,tsx,css}", "!../test/**", "!../**/*.test.{ts,tsx}"], {
  query: "?raw",
  import: "default",
  eager: true,
});

/** Files at the project root that ship with the page or set its style. */
const rootFiles = import.meta.glob<string>(["../../index.html", "../../tailwind.config.ts"], {
  query: "?raw",
  import: "default",
  eager: true,
});

const manifests = import.meta.glob<string>(["../../package.json", "../../tsconfig.json"], {
  query: "?raw",
  import: "default",
  eager: true,
});

const manifest = (name: string): Record<string, unknown> => {
  const entry = Object.entries(manifests).find(([path]) => path.endsWith(name));
  if (!entry) throw new Error(`${name} was not found`);
  return JSON.parse(entry[1]) as Record<string, unknown>;
};

/**
 * Removes `/* *\/` and `//` comments, so a sentence that mentions a banned word is
 * not mistaken for code. A `//` after a colon is a URL and stays.
 *
 * HTML comments are not removed. `index.html` is scanned as it is, and it has no
 * comment that names a banned word.
 */
function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1");
}

const FORBIDDEN: [string, RegExp][] = [
  ["localStorage", /\blocalStorage\b/],
  ["sessionStorage", /\bsessionStorage\b/],
  ["document.cookie", /\bdocument\s*\.\s*cookie\b/],
  ["indexedDB", /\bindexedDB\b/],
  ["console call", /\bconsole\s*\.\s*(log|info|warn|error|debug|trace|table|dir|group|time)\b/],
  ["pure red", /#?\bff0000\b|rgb\(\s*255\s*,\s*0\s*,\s*0\s*\)/i],
  ["history change", /\bhistory\s*\.\s*(pushState|replaceState)\b/],
  ["URL assignment", /\blocation\s*\.\s*(href|hash|search)\s*=[^=]/],
  ["mock switch", /\b(VITE_MOCK|MOCK_MODE|USE_MOCK|mockData|mockApi|fakeView)\b/i],
  ["import from the test folder", /from\s+["'][^"']*\/test\/[^"']*["']/],
  ["a fabricated vault view", /\bvault_id\s*:\s*["'`]/],
];

/** What is wrong with one source text, by rule name. */
function violationsIn(source: string): string[] {
  const code = stripComments(source);
  return FORBIDDEN.filter(([, pattern]) => pattern.test(code)).map(([name]) => name);
}

describe("the source", () => {
  it("reads the production files it is meant to check", () => {
    const names = Object.keys(production);
    expect(names.length).toBeGreaterThan(25);
    for (const expected of ["../lib/api.ts", "../lib/useSimulator.ts", "../components/UserPhonePanel.tsx", "../App.tsx", "../index.css"]) {
      expect(names).toContain(expected);
    }
    expect(names.some((name) => name.includes("/test/"))).toBe(false);
    expect(names.some((name) => /\.test\.tsx?$/.test(name))).toBe(false);
  });

  it("catches each forbidden pattern when it is present, and ignores it in a comment", () => {
    expect(violationsIn("localStorage.setItem('a', 'b')")).toEqual(["localStorage"]);
    expect(violationsIn("window.sessionStorage")).toEqual(["sessionStorage"]);
    expect(violationsIn("document.cookie = 'a=b'")).toEqual(["document.cookie"]);
    expect(violationsIn("console.log(token)")).toEqual(["console call"]);
    expect(violationsIn("color: #FF0000;")).toEqual(["pure red"]);
    expect(violationsIn("history.pushState({}, '', '?v=1')")).toEqual(["history change"]);
    expect(violationsIn("const view = { vault_id: 'x' }")).toEqual(["a fabricated vault view"]);
    expect(violationsIn('import { makeView } from "../test/stubs";')).toEqual(["import from the test folder"]);

    expect(violationsIn("// localStorage and console.log are not used\nconst a = 1;")).toEqual([]);
    expect(violationsIn("/* document.cookie, #FF0000 */ const a = 1;")).toEqual([]);
    expect(violationsIn("The console. An error says nothing.")).toEqual([]);
    expect(violationsIn('const url = "https://example.com"; console.log(url)')).toEqual(["console call"]);
  });

  it("writes nothing to the console, storage, cookies, the URL or IndexedDB, and uses no pure red", () => {
    const all = { ...production, ...rootFiles };
    const problems = Object.entries(all).flatMap(([name, source]) => violationsIn(source).map((rule) => `${name}: ${rule}`));

    expect(problems).toEqual([]);
  });

  it("keeps the Block & Report red to the two Block & Report buttons", () => {
    const usesRed = (source: string): boolean => /\bveto-red\b|\bred-\d{2,3}\b|\bBUTTON_VETO\b/.test(stripComments(source));
    const files = Object.entries(production)
      .filter(([, source]) => usesRed(source))
      .map(([name]) => name)
      .sort();

    // ui.ts defines the red button. The two panels each have one Block & Report button.
    expect(files).toEqual(["../components/GuardianPhonePanel.tsx", "../components/UserPhonePanel.tsx", "../components/ui.ts"]);
  });
});

// ---------------------------------------------------------------------------
// Dependencies and project settings
// ---------------------------------------------------------------------------

describe("the project", () => {
  it("depends on React and lucide-react only, and has no state library", () => {
    const pkg = manifest("package.json") as { dependencies: Record<string, string>; devDependencies: Record<string, string> };

    expect(Object.keys(pkg.dependencies).sort()).toEqual(["lucide-react", "react", "react-dom"]);

    const banned = /^(redux|react-redux|@reduxjs\/|@angular\/|@ngrx\/|zustand|mobx|mobx-react|recoil|jotai|valtio|xstate|rxjs|vuex|pinia|swr|react-query|@tanstack\/react-query|axios)/;
    const all = [...Object.keys(pkg.dependencies), ...Object.keys(pkg.devDependencies)];
    expect(all.filter((name) => banned.test(name))).toEqual([]);
  });

  it("has the npm scripts the build and the tests rely on", () => {
    const pkg = manifest("package.json") as { scripts: Record<string, string> };

    for (const name of ["dev", "build", "preview", "test", "typecheck"]) expect(pkg.scripts).toHaveProperty(name);
    expect(pkg.scripts.test).toBe("vitest run");
    expect(pkg.scripts.typecheck).toBe("tsc --noEmit");
  });

  it("uses strict TypeScript", () => {
    const tsconfig = manifest("tsconfig.json") as { compilerOptions: { strict: boolean } };
    expect(tsconfig.compilerOptions.strict).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Color contrast (WCAG 2.x relative luminance)
// ---------------------------------------------------------------------------

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

/** The theme colors by class name, and the few plain palette colors the app uses. */
const THEME = (tailwindConfig.theme?.extend?.colors ?? {}) as Record<string, string>;
const PALETTE: Record<string, string> = { ...THEME, white: "#FFFFFF", "slate-50": "#F8FAFC", "slate-100": "#F1F5F9" };

/** The page's own colors from `index.css`: its background and its default text. */
const PAGE_BACKGROUND = "#F8FAFC";
const PAGE_TEXT = THEME["slate-surface"] as string;

describe("color contrast", () => {
  it("measures the ratio the way WCAG does", () => {
    expect(contrast("#000000", "#FFFFFF")).toBeCloseTo(21, 1);
    expect(contrast("#FFFFFF", "#FFFFFF")).toBeCloseTo(1, 5);
    expect(contrast("#92400E", "#FEF3C7")).toBeCloseTo(6.37, 1);
  });

  it("reads the colors the design names", () => {
    expect(THEME).toMatchObject({
      "amber-surface": "#FEF3C7",
      "slate-surface": "#1E293B",
      "slate-ink": "#F8FAFC",
      "vault-teal": "#0F766E",
      "veto-red": "#DC2626",
    });
    expect(Object.values(THEME).some((color) => /^#ff0000$/i.test(color))).toBe(false);
  });

  it.each([
    ["alert text on the amber surface", "amber-text", "amber-surface"],
    ["text on the calm slate surface", "slate-ink", "slate-surface"],
    ["text on a white phone screen", "slate-surface", "white"],
    ["text on the page", "slate-surface", "slate-50"],
    ["the vault card text and the countdown", "white", "vault-deep"],
    ["a button label on the vault card", "vault-deep", "white"],
    ["a button label on the amber answers", "amber-text", "white"],
    ["a primary button label", "slate-ink", "slate-surface"],
  ])("has at least 7:1 for %s", (_name, text, background) => {
    expect(contrast(PALETTE[text] as string, PALETTE[background] as string)).toBeGreaterThanOrEqual(7);
  });

  it("keeps the colors that fall short of 7:1 off all text", () => {
    // These are in the theme, but each is too low for text and is used for something else.
    expect(contrast(PALETTE["amber-ink"] as string, PALETTE["amber-surface"] as string)).toBeLessThan(7); // icons and borders
    expect(contrast(PALETTE["white"] as string, PALETTE["vault-teal"] as string)).toBeLessThan(7); // the card frame
    expect(contrast(PALETTE["white"] as string, PALETTE["veto-red"] as string)).toBeGreaterThanOrEqual(4.5); // large bold labels only
  });
});

// ---------------------------------------------------------------------------
// The running app: scenes that cover every screen and message
// ---------------------------------------------------------------------------

/** Shows a vault in the given state on both phones, through the evaluate answer. */
async function showState(fixture: ReturnType<typeof view>) {
  const app = renderApp();
  const { fetchStub } = app;
  fetchStub.respond(200, fixture);
  fireEvent.click(presetButton(SAFE_BUTTON));
  await waitFor(() => expect(presetButton(SAFE_BUTTON)).toBeEnabled());
  await waitFor(() => expect(screen.queryByText("Choose a payment above to see how RukPay keeps it safe.")).not.toBeInTheDocument());
  return app;
}

const SCENES: [string, () => Promise<unknown>][] = [
  ["nothing chosen yet", async () => void renderApp()],
  ["INSTANT_PASS", () => showState(view("INSTANT_PASS"))],
  ["REALITY_CHECK with the questions open", () => showState(view("REALITY_CHECK"))],
  ["REALITY_CHECK with the checks passed", () => showState(view("REALITY_CHECK", { settled: true }))],
  ["LOCKED_15M", () => showState(locked())],
  ["GUARDIAN_APPROVED", () => showState(view("GUARDIAN_APPROVED", { settled: true }))],
  ["VETOED", () => showState(view("VETOED"))],
  ["EXPIRED that needs the final confirmation", () => showState(view("EXPIRED", { reconfirm_required: true }))],
  ["EXPIRED that is done", () => showState(view("EXPIRED", { reconfirm_required: false, settled: true }))],
  [
    "the guardian's second step",
    async () => {
      await showState(locked());
      fireEvent.click(guardianPhone().getByRole("button", { name: "I Spoke With Them — Safe to Approve" }));
    },
  ],
  [
    "the unreachable notice",
    async () => {
      const { fetchStub } = renderApp();
      fetchStub.reject();
      fireEvent.click(presetButton(HIGH_BUTTON));
      await userPhone().findByText(/We can't reach RukPay/);
    },
  ],
  [
    "a calm message from the server (HTTP 400)",
    async () => {
      const { fetchStub } = renderApp();
      fetchStub.respond(400, { error: { code: "invalid_field", message: "Please check the amount and try again." } });
      fireEvent.click(presetButton(HIGH_BUTTON));
      await userPhone().findByText("Please check the amount and try again.");
    },
  ],
  [
    "the approval link notice with its Refresh button (HTTP 403)",
    async () => {
      const { fetchStub } = await showState(locked());
      fetchStub.respond(403, { error: { code: "forbidden", message: "Not valid." } });
      fireEvent.click(guardianPhone().getByRole("button", { name: "I Spoke With Them — Safe to Approve" }));
      fireEvent.click(guardianPhone().getByRole("button", { name: "Yes, approve" }));
      await guardianPhone().findByRole("button", { name: "Refresh" });
    },
  ],
  [
    "the already-acted notice on both phones (HTTP 409)",
    async () => {
      const { fetchStub } = await showState(locked());
      fetchStub.respond(409, {
        error: { code: "state_conflict", message: "Moved on." },
        vault: view("VETOED", { evidence_summary: "Summary." }),
      });
      fireEvent.click(guardianPhone().getByRole("button", { name: "I Spoke With Them — Safe to Approve" }));
      fireEvent.click(guardianPhone().getByRole("button", { name: "Yes, approve" }));
      await guardianPhone().findByText("Someone already acted on this payment.");
    },
  ],
  [
    "the second reality-check question",
    async () => {
      await showState(view("REALITY_CHECK"));
      fireEvent.click(screen.getByRole("button", { name: "No" }));
    },
  ],
  [
    "the copied message on the evidence card",
    async () => {
      await showState(view("VETOED"));
      fireEvent.click(userPhone().getByRole("button", { name: "Copy summary" }));
      await userPhone().findAllByText(/Summary copied\.|copy it by hand/);
    },
  ],
];

/** Runs `check` on every scene, one at a time, with a clean page for each. */
async function inEveryScene(check: (scene: string) => void): Promise<void> {
  for (const [name, setup] of SCENES) {
    await setup();
    check(name);
    cleanup();
  }
}

describe("the app while it runs", () => {
  it("writes no cookie, no storage item, no history entry and no address change through a locked-to-approved flow", async () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    const removeItem = vi.spyOn(Storage.prototype, "removeItem");
    const cookieSetter = vi.spyOn(Document.prototype, "cookie", "set");
    const pushState = vi.spyOn(window.history, "pushState");
    const replaceState = vi.spyOn(window.history, "replaceState");
    const address = window.location.href;

    const { user, fetchStub } = renderApp();
    fetchStub.respond(200, locked());
    await user.click(presetButton(HIGH_BUTTON));
    await userPhone().findByRole("timer");
    await user.click(guardianPhone().getByRole("button", { name: "I Spoke With Them — Safe to Approve" }));
    fetchStub.respond(200, { status: "OK", vault: view("GUARDIAN_APPROVED", { settled: true }) });
    await user.click(guardianPhone().getByRole("button", { name: "Yes, approve" }));
    await guardianPhone().findByText("Payment approved");

    expect(setItem).not.toHaveBeenCalled();
    expect(removeItem).not.toHaveBeenCalled();
    expect(cookieSetter).not.toHaveBeenCalled();
    expect(pushState).not.toHaveBeenCalled();
    expect(replaceState).not.toHaveBeenCalled();
    expect(window.location.href).toBe(address);
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
    expect(document.cookie).toBe("");
  });

  it("never puts the guardian token in the page", async () => {
    await showState(locked({ guardian_token: "secret-guardian-token-xyz" }));

    expect(document.body.innerHTML).not.toContain("secret-guardian-token-xyz");
    expect(window.location.href).not.toContain("secret-guardian-token-xyz");
  });

  it("gives every button and link a hit area of at least 48 by 48 pixels, by class", async () => {
    await inEveryScene((scene) => {
      const controls = Array.from(document.querySelectorAll<HTMLElement>("button, a[href]"));
      expect(controls.length, scene).toBeGreaterThan(0);
      for (const control of controls) {
        expect(control, `${scene}: ${control.textContent?.trim()}`).toHaveClass("min-h-12");
        expect(control, `${scene}: ${control.textContent?.trim()}`).toHaveClass("min-w-12");
      }
    });
  });

  it("leaves at least 12 pixels between neighbouring controls, by class", async () => {
    await inEveryScene((scene) => {
      const parents = new Set(
        Array.from(document.querySelectorAll<HTMLElement>("button, a[href]"))
          .map((control) => control.parentElement)
          .filter((parent): parent is HTMLElement => parent !== null),
      );
      for (const parent of parents) {
        const neighbours = Array.from(parent.children).filter((child) => child.matches("button, a[href]"));
        if (neighbours.length < 2) continue;
        // gap-3 is 12 pixels. Anything from gap-3 up is enough.
        const gap = Array.from(parent.classList).some((name) => /^gap-(3|4|5|6|7|8|9|10|11|12)$/.test(name));
        expect(gap, `${scene}: controls in <${parent.tagName.toLowerCase()} class="${parent.className}"> are too close`).toBe(true);
      }
    });
  });

  it("gives every icon-only control an accessible name, and hides the icons that only decorate", async () => {
    await inEveryScene((scene) => {
      for (const control of Array.from(document.querySelectorAll<HTMLElement>("button, a[href]"))) {
        const name = control.getAttribute("aria-label") ?? control.textContent?.trim() ?? "";
        expect(name.length, `${scene}: a control has no name`).toBeGreaterThan(0);
      }
      for (const icon of Array.from(document.querySelectorAll("svg"))) {
        expect(icon.getAttribute("aria-hidden"), `${scene}: an icon is not hidden from screen readers`).toBe("true");
      }
    });
  });

  it("shows no pure red anywhere, and red only on the two Block & Report buttons", async () => {
    await inEveryScene((scene) => {
      expect(document.body.innerHTML, scene).not.toMatch(/#ff0000|rgb\(255,\s*0,\s*0\)/i);
      const red = Array.from(document.querySelectorAll<HTMLElement>("[class]")).filter((el) =>
        /\b(bg|text|border)-(veto-red|red-\d{2,3})\b/.test(el.className),
      );
      for (const el of red) {
        expect(el.textContent, scene).toMatch(/^\s*Block & Report/);
        expect(el.tagName, scene).toBe("BUTTON");
      }
    });
  });

  describe("color contrast of the text on screen", () => {
    const TEXT_SIZE = /^text-(xs|sm|base|lg|xl|[2-9]xl)$/;
    const NOT_A_COLOR = /^(left|right|center|justify|start|end|wrap|nowrap|balance|pretty|ellipsis|clip)$/;

    /** A color class without a variant prefix, such as `text-white`, as its palette color. */
    function colorFromClass(el: Element, prefix: "text" | "bg"): string | undefined {
      for (const name of Array.from(el.classList)) {
        if (name.includes(":") || !name.startsWith(`${prefix}-`)) continue;
        const token = name.slice(prefix.length + 1);
        if (prefix === "text" && (TEXT_SIZE.test(name) || NOT_A_COLOR.test(token))) continue;
        const color = PALETTE[token];
        if (!color) throw new Error(`The class "${name}" is not in the contrast check's palette. Add it, or use a theme color.`);
        return color;
      }
      return undefined;
    }

    /** The nearest color of its kind on the element or above it. */
    function effective(el: Element, prefix: "text" | "bg", fallback: string): string {
      for (let node: Element | null = el; node; node = node.parentElement) {
        const color = colorFromClass(node, prefix);
        if (color) return color;
      }
      return fallback;
    }

    const hasOwnText = (el: Element): boolean =>
      Array.from(el.childNodes).some((node) => node.nodeType === Node.TEXT_NODE && (node.textContent ?? "").trim() !== "");

    it("is at least 7:1 for all text, and at least 4.5:1 for the large bold Block & Report labels", async () => {
      let checked = 0;
      await inEveryScene((scene) => {
        for (const el of Array.from(document.body.querySelectorAll("*"))) {
          if (!hasOwnText(el)) continue;
          const text = effective(el, "text", PAGE_TEXT);
          const background = effective(el, "bg", PAGE_BACKGROUND);
          const ratio = contrast(text, background);
          const label = `${scene}: "${el.textContent?.trim().slice(0, 40)}" is ${ratio.toFixed(2)}:1`;
          checked += 1;

          if (background === THEME["veto-red"]) {
            // The red button is the one place white on color is under 7:1, so its label must be large and bold.
            const button = el.closest("button");
            expect(button, label).not.toBeNull();
            expect(button, label).toHaveClass("text-xl", "font-bold");
            expect(ratio, label).toBeGreaterThanOrEqual(4.5);
          } else {
            expect(ratio, label).toBeGreaterThanOrEqual(7);
          }
        }
      });
      expect(checked).toBeGreaterThan(300);
    });

    it("finds a low-contrast pair when there is one", () => {
      const probe = document.createElement("div");
      probe.className = "bg-vault-teal";
      probe.innerHTML = '<p class="text-white">Low</p>';
      document.body.append(probe);
      const text = probe.querySelector("p") as Element;

      expect(contrast(effective(text, "text", PAGE_TEXT), effective(text, "bg", PAGE_BACKGROUND))).toBeLessThan(7);
      probe.remove();
    });
  });
});
