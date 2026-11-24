import { Ban, CheckCircle2, Clock, HelpCircle, Vault, type LucideIcon } from "lucide-react";
import { useState } from "react";

import { COPY } from "../lib/copy";
import { formatRupees } from "../lib/format";
import { deriveUserScreen, type UserScreen } from "../lib/screens";
import type { Simulator } from "../lib/useSimulator";
import { CoolingOffVaultCard } from "./CoolingOffVaultCard";
import { EvidenceCard } from "./EvidenceCard";
import { ExplanationCard } from "./ExplanationCard";
import { NoticeBlock } from "./NoticeBlock";
import { PhoneFrame } from "./PhoneFrame";
import { RealityCheckModal } from "./RealityCheckModal";
import { StatusLine } from "./StatusLine";
import { UnreachableNotice } from "./UnreachableNotice";
import { BUTTON_PRIMARY, BUTTON_SECONDARY, BUTTON_VETO } from "./ui";
import { useGuard } from "./useGuard";

const STATUS_ICON: Record<UserScreen, LucideIcon> = {
  PROMPT: HelpCircle,
  SENT: CheckCircle2,
  CHECKING: HelpCircle,
  CONTINUE: CheckCircle2,
  LOCKED: Vault,
  RELEASED: CheckCircle2,
  CANCELLED: Ban,
  RECONFIRM: Clock,
  DONE: CheckCircle2,
};

/**
 * The payer's phone. What it shows follows the vault: the payee and amount, a
 * status line with an icon, the reason in plain words, and the one screen for
 * the current state. Notices stay inline until the state changes or the person
 * acts. Nothing here disappears by itself.
 */
export function UserPhonePanel({ sim }: { sim: Simulator }) {
  const { state } = sim;
  const { vault, preset } = state;
  const run = useGuard();

  // "Continue payment" has no request. It only moves this screen on to the
  // payment-sent confirmation, for the vault it was pressed on.
  const [acknowledgedVault, setAcknowledgedVault] = useState<string | null>(null);

  let screen: UserScreen = deriveUserScreen(vault);
  if (screen === "RECONFIRM" && state.reconfirmCancelled) screen = "PROMPT";
  if (screen === "CONTINUE" && vault && acknowledgedVault === vault.vault_id) screen = "SENT";

  const amount = vault?.metadata.amount_inr ?? preset?.request.amount_inr;
  const showPayment = preset !== null && screen !== "PROMPT";
  // Link and clipboard notices belong to the guardian phone and the evidence card.
  const notice = state.notice?.kind === "BAD_REQUEST" || state.notice?.kind === "ALREADY_ACTED" ? state.notice : null;

  return (
    <PhoneFrame label={COPY.app.userPhoneLabel}>
      {showPayment && (
        <div className="flex flex-col gap-1 rounded-2xl bg-slate-100 p-4">
          <p className="text-base">{COPY.userPanel.payingLabel}</p>
          <p className="text-xl font-bold">{preset.payeeLabel}</p>
          <p className="text-lg tabular-nums">{vault?.metadata.beneficiary_masked ?? vault?.metadata.upi_masked ?? preset.payeeMasked}</p>
          <p className="text-base">{COPY.userPanel.amountLabel}</p>
          <p className="text-3xl font-bold tabular-nums">{amount === undefined ? "" : formatRupees(amount)}</p>
          <p className="text-base">{preset.timeLabel}</p>
        </div>
      )}

      <StatusLine icon={STATUS_ICON[screen]} text={COPY.userPanel.status[screen]} />
      {vault && screen !== "PROMPT" && vault.reason !== "" && <p className="text-lg">{vault.reason}</p>}

      {notice && <NoticeBlock message={notice.message} />}
      {state.unreachable && screen !== "CHECKING" && <UnreachableNotice onRetry={sim.retry} />}

      {screen === "PROMPT" && <p className="text-lg">{COPY.userPanel.prompt}</p>}

      {screen === "SENT" && <p className="text-lg font-semibold">{COPY.userPanel.sentMessage}</p>}

      {screen === "CONTINUE" && vault && (
        <>
          <p className="text-lg font-semibold">{COPY.userPanel.continueMessage}</p>
          <button type="button" onClick={() => setAcknowledgedVault(vault.vault_id)} className={BUTTON_PRIMARY}>
            {COPY.userPanel.continueButton}
          </button>
        </>
      )}

      {screen === "LOCKED" && vault && (
        <>
          <CoolingOffVaultCard
            view={vault}
            onExpire={() => run(sim.refresh)}
            onFastForward={() => run(sim.fastForward)}
            fastForwardHidden={state.fastForwardHidden}
          />
          <p className="text-base">{COPY.userPanel.blockHint}</p>
          <button type="button" onClick={() => run(sim.veto)} className={BUTTON_VETO}>
            {COPY.userPanel.blockButton}
          </button>
        </>
      )}

      {screen === "RELEASED" && <p className="text-lg font-semibold">{COPY.userPanel.releasedMessage}</p>}

      {screen === "CANCELLED" && <EvidenceCard summary={vault?.evidence_summary} />}

      {screen === "RECONFIRM" && (
        <>
          <p className="text-lg font-semibold">{COPY.userPanel.reconfirmMessage}</p>
          <div className="flex flex-col gap-3">
            <button type="button" onClick={() => run(sim.reconfirm)} className={BUTTON_PRIMARY}>
              {COPY.userPanel.reconfirmYes}
            </button>
            <button type="button" onClick={sim.cancelReconfirm} className={BUTTON_SECONDARY}>
              {COPY.userPanel.reconfirmCancel}
            </button>
          </div>
        </>
      )}

      {screen === "DONE" && <p className="text-lg font-semibold">{COPY.userPanel.doneMessage}</p>}

      {/* Why the payment was scored this way. It stays through every state change and goes only with the vault. */}
      {vault && <ExplanationCard view={vault} />}

      <RealityCheckModal
        open={screen === "CHECKING"}
        onSubmit={sim.submitAnswers}
        unreachable={state.unreachable !== null}
        onRetry={sim.retry}
      />
    </PhoneFrame>
  );
}
