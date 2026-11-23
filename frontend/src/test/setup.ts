import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

import { restoreStubs } from "./stubs";

// Vitest runs without globals here, so Testing Library cannot register its own
// cleanup. Unmount every rendered tree after each test, put back anything a test
// spied on, and put back any fetch or EventSource stub it installed.
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  restoreStubs();
});
