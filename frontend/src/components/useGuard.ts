import { useCallback, useRef } from "react";

/**
 * Runs an async action at most once at a time. A second tap on a button while
 * the first request is still going does nothing, so a trembling hand cannot
 * send the same request twice. The button stays enabled and keeps its focus.
 */
export function useGuard(): (action: () => Promise<void>) => void {
  const running = useRef(false);
  return useCallback((action) => {
    if (running.current) return;
    running.current = true;
    void action().finally(() => {
      running.current = false;
    });
  }, []);
}
