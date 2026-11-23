import { act, fireEvent, render, screen } from "@testing-library/react";
import fc from "fast-check";
import { describe, expect, it } from "vitest";

import { submitAnswers, type Answer } from "../lib/api";
import { installFetchStub, makeView } from "../test/stubs";
import { RealityCheckModal } from "./RealityCheckModal";

const answer: fc.Arbitrary<Answer> = fc.constantFrom("YES", "NO");
const label = (a: Answer): string => (a === "YES" ? "Yes" : "No");

/** Lets the request finish: every pending promise settles within one timer tick. */
const settle = (): Promise<void> =>
  act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });

// 100 renders are slow when the whole suite runs at once, so these get more than the default 5 seconds.
const TIMEOUT_MS = 60_000;

describe("reality check properties", () => {
  // Feature: dual-screen-simulator, Property 13: Reality-check body reflects the answers
  // Validates: Requirements 4.4
  it("Property 13: any three answers produce exactly the body { answers: { coercion, secrecy, verification } }", async () => {
    await fc.assert(
      fc.asyncProperty(answer, answer, answer, async (first, second, third) => {
        const fetchStub = installFetchStub().respond(200, makeView("LOCKED_15M"));
        const view = render(
          <RealityCheckModal
            open
            unreachable={false}
            onRetry={() => undefined}
            onSubmit={async (answers) => {
              await submitAnswers("vault-1", answers);
            }}
          />,
        );

        for (const a of [first, second, third]) {
          fireEvent.click(screen.getByRole("button", { name: label(a) }));
        }
        await settle();
        expect(fetchStub.calls).toHaveLength(1);

        const body = fetchStub.calls[0]?.body;
        expect(body).toEqual({ answers: { coercion: first, secrecy: second, verification: third } });
        expect(Object.keys(body as object)).toEqual(["answers"]);
        expect(Object.keys((body as { answers: object }).answers).sort()).toEqual(["coercion", "secrecy", "verification"]);

        view.unmount();
      }),
      { numRuns: 100 },
    );
  }, TIMEOUT_MS);

  it("Property 13: going back and changing an answer still sends the final three", async () => {
    await fc.assert(
      fc.asyncProperty(answer, answer, answer, answer, async (first, changed, second, third) => {
        const fetchStub = installFetchStub().respond(200, makeView("LOCKED_15M"));
        const view = render(
          <RealityCheckModal
            open
            unreachable={false}
            onRetry={() => undefined}
            onSubmit={async (answers) => {
              await submitAnswers("vault-1", answers);
            }}
          />,
        );

        fireEvent.click(screen.getByRole("button", { name: label(first) }));
        fireEvent.click(screen.getByRole("button", { name: "Back" }));
        for (const a of [changed, second, third]) {
          fireEvent.click(screen.getByRole("button", { name: label(a) }));
        }
        await settle();
        expect(fetchStub.calls).toHaveLength(1);

        expect(fetchStub.calls[0]?.body).toEqual({
          answers: { coercion: changed, secrecy: second, verification: third },
        });
        view.unmount();
      }),
      { numRuns: 100 },
    );
  }, TIMEOUT_MS);
});
