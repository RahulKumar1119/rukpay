import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { COPY } from "../lib/copy";
import { SimulationNotice } from "./SimulationNotice";
import { NOTICE_BOX } from "./ui";

afterEach(() => {
  vi.useRealTimers();
});

const SENTENCE = "This is a simulation of how RukPay treats payments. No real payment is made, held or cancelled.";

describe("SimulationNotice", () => {
  it("shows the one sentence from copy, and nothing else", () => {
    const { container } = render(<SimulationNotice />);

    expect(COPY.simulationNotice).toBe(SENTENCE);
    expect(screen.getByText(SENTENCE)).toBeInTheDocument();
    // No other text, and no label, title or placeholder that holds a literal of its own.
    expect(container.textContent).toBe(SENTENCE);
    expect(container.querySelectorAll("[aria-label], [title], [placeholder], [alt]")).toHaveLength(0);
  });

  it("is an amber notice box, readable as a note", () => {
    render(<SimulationNotice />);
    const note = screen.getByRole("note");

    for (const name of NOTICE_BOX.split(/\s+/)) expect(note).toHaveClass(name);
    expect(note).toHaveClass("bg-amber-surface", "text-amber-text");
    expect(note).toHaveTextContent(SENTENCE);
  });

  it("puts a decorative Info icon before the sentence", () => {
    const { container } = render(<SimulationNotice />);

    const icons = container.querySelectorAll("svg");
    expect(icons).toHaveLength(1);
    const icon = icons[0] as SVGElement;
    expect(icon).toHaveAttribute("aria-hidden", "true");
    expect(icon.getAttribute("class")).toContain("lucide-info");
    const sentence = screen.getByText(SENTENCE);
    expect(icon.compareDocumentPosition(sentence) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("has no close control and nothing to press, type into or tab to", () => {
    const { container } = render(<SimulationNotice />);

    expect(screen.queryAllByRole("button")).toHaveLength(0);
    expect(screen.queryAllByRole("link")).toHaveLength(0);
    expect(container.querySelectorAll("button, a, input, select, textarea, summary, [role=button], [tabindex], [onclick]")).toHaveLength(0);
    expect(container.innerHTML).not.toMatch(/close|dismiss|hide/i);
  });

  it("starts no timer and stays after a long time", () => {
    vi.useFakeTimers();
    render(<SimulationNotice />);

    expect(vi.getTimerCount()).toBe(0);
    vi.advanceTimersByTime(60 * 60 * 1000);
    expect(screen.getByText(SENTENCE)).toBeInTheDocument();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("stays after a click, a key press and a new render", () => {
    const { rerender } = render(<SimulationNotice />);
    const note = screen.getByRole("note");

    fireEvent.click(note);
    fireEvent.keyDown(note, { key: "Escape" });
    fireEvent.keyDown(document.body, { key: "Escape" });
    rerender(<SimulationNotice />);

    expect(screen.getByRole("note")).toBeInTheDocument();
    expect(screen.getByText(SENTENCE)).toBeInTheDocument();
  });

  it("takes its sentence from copy and holds no literal text of its own", () => {
    const source = Object.values(import.meta.glob<string>("./SimulationNotice.tsx", { query: "?raw", import: "default", eager: true }))[0] as string;
    const code = source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1");

    expect(code).toContain("COPY.simulationNotice");
    // Text between two tags, other than an expression in braces, would be a literal sentence.
    expect(code.replace(/\{[^{}]*\}/g, "")).not.toMatch(/>\s*[^<>{}\s][^<>{}]*</);
    // Attributes that a person can read or hear are not written out either.
    expect(code).not.toMatch(/\b(aria-label|aria-description|title|placeholder|alt)\s*=/);
    // The icon is hidden from screen readers by the component itself, not only by the icon library's default.
    expect(code).toMatch(/<Info\b[^>]*\baria-hidden="true"/);
  });

  it("holds no word that the new copy rules forbid", () => {
    render(<SimulationNotice />);
    const text = screen.getByRole("note").textContent ?? "";
    expect(text).toMatch(/simulat/i);
    expect(text.toLowerCase()).not.toMatch(/real money|your bank|the bank|money is safe|freeze|frozen|restrict|suspected|warning|alert/);
  });
});
