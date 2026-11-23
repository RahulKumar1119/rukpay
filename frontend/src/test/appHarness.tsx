/**
 * Helpers for tests that render the whole App against the stubbed `fetch` and
 * `EventSource`. Test only.
 */
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import App from "../App";
import type { VaultView } from "../lib/api";
import { installEventSourceStub, installFetchStub, makeView } from "./stubs";

export const VAULT_ID = "vault-app-1";
export const TOKEN = "guardian-token-1";

export function renderApp() {
  const ES = installEventSourceStub();
  const fetchStub = installFetchStub();
  const user = userEvent.setup();
  const utils = render(<App />);
  return { user, ES, fetchStub, ...utils };
}

/** Queries inside the payer's phone. */
export const userPhone = () => within(screen.getByRole("region", { name: "Your phone" }));

/** Queries inside the guardian's phone. */
export const guardianPhone = () => within(screen.getByRole("region", { name: "Family guardian's phone" }));

export const presetButton = (name: string) => screen.getByRole("button", { name });

export const SAFE_BUTTON = "Safe daily payment";
export const MEDIUM_BUTTON = "Medium-risk unusual transfer";
export const HIGH_BUTTON = "High-risk panic transfer";

/** A vault of this test file's id, in any state. */
export function view(state: VaultView["state"], extra: Partial<VaultView> = {}): VaultView {
  return makeView(state, { vault_id: VAULT_ID, ...extra });
}

/** The first View of the high-risk preset: locked, with the guardian token. */
export const locked = (extra: Partial<VaultView> = {}): VaultView =>
  view("LOCKED_15M", { remaining_seconds: 900, guardian_token: TOKEN, ...extra });

/** The calm error body the backend sends. */
export const errorBody = (code: string, message: string) => ({ error: { code, message } });
