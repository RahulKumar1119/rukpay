/**
 * Which screen each phone shows for a given View. Both functions are pure and
 * total: every View, and `null` for "no vault yet", maps to exactly one screen.
 */
import type { VaultView } from "./api";

export type UserScreen =
  | "PROMPT"
  | "SENT"
  | "CHECKING"
  | "CONTINUE"
  | "LOCKED"
  | "RELEASED"
  | "CANCELLED"
  | "RECONFIRM"
  | "DONE";

export type GuardianScreen = "IDLE" | "ALERT" | "APPROVED" | "VETOED";

/**
 * The payer's screen. The reality-check modal is open exactly when this is
 * CHECKING (Requirement 4.1). A state this client does not know shows the prompt.
 */
export function deriveUserScreen(v: VaultView | null): UserScreen {
  if (v === null) return "PROMPT";
  switch (v.state) {
    case "INSTANT_PASS":
      return "SENT";
    case "REALITY_CHECK":
      return v.settled ? "CONTINUE" : "CHECKING";
    case "LOCKED_15M":
      return "LOCKED";
    case "GUARDIAN_APPROVED":
      return "RELEASED";
    case "VETOED":
      return "CANCELLED";
    case "EXPIRED":
      return v.reconfirm_required ? "RECONFIRM" : "DONE";
    default:
      return "PROMPT";
  }
}

/** The guardian's screen. The alert shows exactly while the vault is LOCKED_15M. */
export function deriveGuardianScreen(v: VaultView | null): GuardianScreen {
  if (v === null) return "IDLE";
  switch (v.state) {
    case "LOCKED_15M":
      return "ALERT";
    case "GUARDIAN_APPROVED":
      return "APPROVED";
    case "VETOED":
      return "VETOED";
    default:
      return "IDLE";
  }
}

/**
 * True when the vault can no longer change, so the live updates can stop
 * (Requirement 8.7). An expired vault that still needs the payer's final
 * confirmation is not terminal.
 */
export function isTerminal(v: VaultView): boolean {
  switch (v.state) {
    case "INSTANT_PASS":
    case "GUARDIAN_APPROVED":
    case "VETOED":
      return true;
    case "EXPIRED":
      return !v.reconfirm_required;
    default:
      return false;
  }
}
