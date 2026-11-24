import { CheckCircle2, ListChecks } from "lucide-react";
import { useId } from "react";
import type { ExplanationItem, VaultView } from "../lib/api";
import { COPY, pointsText, totalText } from "../lib/copy";
import { readExplanation } from "../lib/explanation";
import { StatusLine } from "./StatusLine";
/**
 * One line of the explanation: the reason as plain text, and the points as a
 * sign, a whole number and a word, such as "+20 points". The points are never
 * shown by color or an icon alone. React writes `reason` as text, so markup in
 * it appears as received and is never read as HTML. The `signal` is not shown.
 */
export function ExplanationLine({ item }: { item: ExplanationItem }) {
  return (
    <li className="flex flex-col gap-2 rounded-xl border-2 border-slate-surface bg-white p-3">
      <span className="min-w-0 break-words text-base">{item.reason}</span>
      <strong className="w-fit rounded-lg bg-slate-surface px-3 py-1 text-lg font-bold tabular-nums text-slate-ink">
        {pointsText(item.points)}
      </strong>
    </li>
  );
}
/**
 * Why RukPay looked closer at this payment. One line for each item the server
 * sent, in the order received, then a total line that is the score of the same
 * view. An empty list gets one calm sentence. A view with no usable
 * `explanation` gets no card and no error. Nothing here can be hidden, toggled
 * or timed out: the card stays as long as the vault is on the phone.
 */
export function ExplanationCard({ view }: { view: VaultView }) {
  const titleId = useId();
  const read = readExplanation(view);
  if (read.kind === "absent") return null;
  return (
    <section aria-labelledby={titleId} className="flex flex-col gap-3 rounded-2xl border-2 border-slate-surface bg-white p-4">
      <div className="flex items-center gap-3">
        <ListChecks aria-hidden="true" className="h-7 w-7 shrink-0" />
        <h3 id={titleId} className="text-xl font-bold">
          {COPY.explanation.heading}
        </h3>
      </div>
      {read.kind === "empty" ? (
        <StatusLine icon={CheckCircle2} text={COPY.explanation.empty} />
      ) : (
        <>
          <ul className="flex flex-col gap-3">
            {read.items.map((item, index) => (
              <ExplanationLine key={index} item={item} />
            ))}
          </ul>
          <p className="border-t-2 border-slate-surface pt-3 text-xl font-bold tabular-nums">{totalText(view.risk_score)}</p>
        </>
      )}
    </section>
  );
}
