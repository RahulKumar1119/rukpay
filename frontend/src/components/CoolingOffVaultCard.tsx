import { Vault } from "lucide-react";
import { useId } from "react";

import type { VaultView } from "../lib/api";
import { COPY } from "../lib/copy";
import { Countdown } from "./Countdown";
import { BUTTON_VAULT } from "./ui";

export interface CoolingOffVaultCardProps {
  view: VaultView;
  /** Called once when the countdown reaches 00:00. The owner reads the vault again. */
  onExpire(): void;
  onFastForward(): void;
  /** True once the server has said demo fast-forward is off. */
  fastForwardHidden: boolean;
}

/**
 * "Your money is safe in the vault." The deep teal frame (#0F766E) holds a
 * darker panel (#134E4A) that carries all the text, so white text is 9.5:1. The
 * lock is described as a protective pause, never as a restriction.
 */
export function CoolingOffVaultCard({ view, onExpire, onFastForward, fastForwardHidden }: CoolingOffVaultCardProps) {
  const titleId = useId();
  return (
    <section aria-labelledby={titleId} className="flex flex-col gap-3 rounded-3xl bg-vault-teal p-3 text-white">
      <div className="flex flex-col gap-3 rounded-2xl bg-vault-deep p-4">
        <div className="flex items-center gap-3">
          <Vault aria-hidden="true" className="h-8 w-8 shrink-0" />
          <h3 id={titleId} className="text-xl font-bold">
            {COPY.vaultCard.title}
          </h3>
        </div>
        <p className="text-xl font-bold leading-snug">{COPY.vaultCard.pauseMessage}</p>
        <Countdown view={view} onZero={onExpire} />
        <p className="text-lg">{COPY.vaultCard.verifyHint}</p>
      </div>
      {!fastForwardHidden && (
        <button type="button" onClick={onFastForward} className={BUTTON_VAULT}>
          {COPY.vaultCard.fastForward}
        </button>
      )}
    </section>
  );
}
