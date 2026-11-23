/**
 * The 15-minute countdown.
 *
 * The arithmetic is in small pure functions. The backend sends the seconds that
 * remain, so the client turns that into a local end time (now plus the
 * remaining seconds) and counts down from the clock. The count therefore stays
 * right when a timer runs late, and it restarts from the new value whenever a
 * newer View arrives (Requirements 5.2 to 5.4).
 */
import { useEffect, useRef, useState } from "react";

import type { VaultView } from "./api";

/** The label read aloud changes at most this often, in milliseconds (Requirement 5.6). */
export const SPOKEN_REFRESH_MS = 10_000;

/** The local end time, in milliseconds, for a lock with `remainingSeconds` left at `nowMs`. */
export function endTimeFrom(nowMs: number, remainingSeconds: number): number {
  return nowMs + remainingSeconds * 1000;
}

/** Whole seconds left at `nowMs`, rounded up and never below zero. */
export function secondsLeft(endMs: number, nowMs: number): number {
  return Math.max(0, Math.ceil((endMs - nowMs) / 1000));
}

/** `mm:ss`. A negative or non-finite value shows `00:00`, so the display never counts below zero. */
export function formatCountdown(totalSeconds: number): string {
  const whole = Number.isFinite(totalSeconds) ? Math.max(0, Math.trunc(totalSeconds)) : 0;
  const minutes = String(Math.floor(whole / 60)).padStart(2, "0");
  const seconds = String(whole % 60).padStart(2, "0");
  return `${minutes}:${seconds}`;
}

/** The number of seconds in an `mm:ss` text, or NaN when the text is not in that form. */
export function parseCountdown(text: string): number {
  const match = /^(\d{2,}):([0-5]\d)$/.exec(text);
  if (!match) return Number.NaN;
  return Number(match[1]) * 60 + Number(match[2]);
}

const plural = (n: number, unit: string): string => `${n} ${unit}${n === 1 ? "" : "s"}`;

/** The time in words for a screen reader: "14 minutes and 5 seconds". */
export function spokenCountdown(totalSeconds: number): string {
  const whole = Number.isFinite(totalSeconds) ? Math.max(0, Math.trunc(totalSeconds)) : 0;
  const minutes = Math.floor(whole / 60);
  const seconds = whole % 60;
  if (minutes === 0) return plural(seconds, "second");
  if (seconds === 0) return plural(minutes, "minute");
  return `${plural(minutes, "minute")} and ${plural(seconds, "second")}`;
}

/**
 * Counts down a locked vault.
 *
 * - While `view` is LOCKED_15M it ticks once a second from the clock. It starts
 *   again from `remaining_seconds` whenever that value, the vault or the state
 *   changes, which is what a newer View does.
 * - It calls `onZero` once when the count first reaches zero for a vault. The
 *   caller uses it to read the vault one time and show the state the server
 *   returns. Another vault, or a View with time left, arms it again.
 * - `text` is the `mm:ss` display. `spoken` is the same time in words for an
 *   `aria-label`, and it changes at most once every 10 seconds.
 * - When the state is not LOCKED_15M, or `view` is null, it stops its interval
 *   and shows `00:00`. It also stops when the component unmounts.
 */
export function useCountdown(view: VaultView | null, onZero: () => void): { text: string; spoken: string } {
  const locked = view !== null && view.state === "LOCKED_15M";
  const vaultId = view?.vault_id ?? "";
  const remaining = locked ? view.remaining_seconds : 0;

  const [seconds, setSeconds] = useState(remaining);
  const [spoken, setSpoken] = useState(() => spokenCountdown(remaining));

  const onZeroRef = useRef(onZero);
  const firedFor = useRef<string | null>(null);
  const lastSpoken = useRef<{ vaultId: string; at: number } | null>(null);

  useEffect(() => {
    onZeroRef.current = onZero;
  });

  useEffect(() => {
    if (!locked) {
      setSeconds(0);
      setSpoken(spokenCountdown(0));
      lastSpoken.current = null;
      return undefined;
    }

    const endMs = endTimeFrom(Date.now(), remaining);
    if (remaining > 0) firedFor.current = null;

    const tick = (): void => {
      const now = Date.now();
      const left = secondsLeft(endMs, now);
      setSeconds(left);

      const last = lastSpoken.current;
      if (last === null || last.vaultId !== vaultId || now - last.at >= SPOKEN_REFRESH_MS) {
        lastSpoken.current = { vaultId, at: now };
        setSpoken(spokenCountdown(left));
      }

      if (left === 0 && firedFor.current !== vaultId) {
        firedFor.current = vaultId;
        onZeroRef.current();
      }
    };

    tick();
    const timer = setInterval(tick, 1000);
    return () => clearInterval(timer);
  }, [locked, vaultId, remaining]);

  return { text: formatCountdown(seconds), spoken };
}
