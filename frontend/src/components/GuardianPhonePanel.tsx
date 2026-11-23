import { Ban, CheckCircle2, ShieldCheck, Users, RefreshCw } from "lucide-react";
import { useState } from "react";

import type { VaultView } from "../lib/api";
import { COPY } from "../lib/copy";
import { formatRupees } from "../lib/format";
import { deriveGuardianScreen } from "../lib/screens";
import type { Simulator } from "../lib/useSimulator";
import { Countdown } from "./Countdown";
import { EvidenceCard } from "./EvidenceCard";
import { NoticeBlock } from "./NoticeBlock";
import { PhoneFrame } from "./PhoneFrame";
import { StatusLine } from "./StatusLine";
import { BUTTON_PRIMARY, BUTTON_SECONDARY, BUTTON_VETO } from "./ui";
import { useGuard } from "./useGuard";

/**
 * The family guardian's phone. It shows only what is safe to show: the reason in
 * plain words, the amount and a masked identifier. It never shows an account
 * number, a UPI id or a private message.
 */
export function GuardianPhonePanel({ sim }: { sim: Simulator }) {
  const { state } = sim;
  const { vault } = state;
  const screen = deriveGuardianScreen(vault);
  const notice = state.notice?.kind === "LINK_INVALID" || state.notice?.kind === "ALREADY_ACTED" ? state.notice : null;
  const run = useGuard();

  return (
    <PhoneFrame label={COPY.app.guardianPhoneLabel}>
      {screen === "IDLE" && <StatusLine icon={ShieldCheck} text={COPY.guardianPanel.idle} />}

      {screen === "ALERT" && vault && (
        // A new vault starts the approval steps again, because the key changes.
        <GuardianAlert key={vault.vault_id} sim={sim} vault={vault} />
      )}

      {screen === "APPROVED" && (
        <>
          <StatusLine icon={CheckCircle2} text={COPY.guardianPanel.approvedTitle} />
          <p className="text-lg">{COPY.guardianPanel.approvedBody}</p>
        </>
      )}

      {screen === "VETOED" && <EvidenceCard summary={vault?.evidence_summary} />}

      {notice && (
        <NoticeBlock message={notice.message}>
          {notice.kind === "LINK_INVALID" && (
            <button type="button" onClick={() => run(sim.refresh)} className={BUTTON_SECONDARY}>
              <RefreshCw aria-hidden="true" className="h-5 w-5" />
              {COPY.guardianPanel.refresh}
            </button>
          )}
        </NoticeBlock>
      )}
    </PhoneFrame>
  );
}

/**
 * A locked payment that needs the guardian. Approving takes two taps: the first
 * only asks "Are you sure you spoke with them directly?", and nothing is sent
 * until the second. Blocking is always one tap away.
 */
function GuardianAlert({ sim, vault }: { sim: Simulator; vault: VaultView }) {
  const { preset } = sim.state;
  const [confirming, setConfirming] = useState(false);
  const run = useGuard();

  const payee = vault.metadata.beneficiary_masked ?? vault.metadata.upi_masked ?? preset?.payeeMasked;
  const amount = vault.metadata.amount_inr ?? preset?.request.amount_inr;

  return (
    <>
      <StatusLine icon={Users} text={COPY.guardianPanel.alertTitle} />
      <p className="text-lg">{COPY.guardianPanel.alertIntro}</p>

      <div className="flex flex-col gap-1 rounded-2xl bg-slate-100 p-4">
        {vault.reason !== "" && <p className="text-lg font-semibold">{vault.reason}</p>}
        <p className="text-base">{COPY.guardianPanel.amountLabel}</p>
        <p className="text-3xl font-bold tabular-nums">{amount === undefined ? "" : formatRupees(amount)}</p>
        {payee && (
          <>
            <p className="text-base">{COPY.guardianPanel.payingLabel}</p>
            <p className="text-lg tabular-nums">{payee}</p>
          </>
        )}
      </div>

      <Countdown view={vault} onZero={() => undefined} />

      {confirming ? (
        <div className="flex flex-col gap-3 rounded-2xl border-2 border-slate-surface p-4">
          <p className="text-xl font-semibold">{COPY.guardianPanel.confirmQuestion}</p>
          <button type="button" onClick={() => run(() => sim.guardianApprove(true))} className={BUTTON_PRIMARY}>
            {COPY.guardianPanel.confirmYes}
          </button>
          <button type="button" onClick={() => setConfirming(false)} className={BUTTON_SECONDARY}>
            {COPY.guardianPanel.confirmBack}
          </button>
        </div>
      ) : (
        <button type="button" onClick={() => setConfirming(true)} className={BUTTON_PRIMARY}>
          {COPY.guardianPanel.approve}
        </button>
      )}

      <button type="button" onClick={() => run(sim.guardianVeto)} className={BUTTON_VETO}>
        <Ban aria-hidden="true" className="h-6 w-6" />
        {COPY.guardianPanel.veto}
      </button>
    </>
  );
}
