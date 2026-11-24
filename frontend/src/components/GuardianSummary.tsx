import { useId } from "react";
import type { VaultView } from "../lib/api";
import { COPY } from "../lib/copy";
import { guardianTop, readExplanation } from "../lib/explanation";
import { ExplanationLine } from "./ExplanationCard";
/**
 * What RukPay noticed, for the family guardian: up to three of the reasons that
 * added points, the largest first. It shows only while the vault is
 * LOCKED_15M, and only reasons and points. It holds no `signal` and no
 * identifier. With no usable `explanation`, or nothing that added points, it
 * shows nothing and no error.
 */
export function GuardianSummary({ view }: { view: VaultView }) {
  const titleId = useId();
  const read = readExplanation(view);
  if (view.state !== "LOCKED_15M" || read.kind !== "items") return null;
  const top = guardianTop(read.items);
  if (top.length === 0) return null;
  return (
    <section aria-labelledby={titleId} className="mt-2 flex flex-col gap-3 border-t-2 border-slate-surface pt-3">
      <h3 id={titleId} className="text-lg font-bold">
        {COPY.explanation.guardianHeading}
      </h3>
      <ul className="flex flex-col gap-2">
        {top.map((item, index) => (
          <ExplanationLine key={index} item={item} />
        ))}
      </ul>
    </section>
  );
}
