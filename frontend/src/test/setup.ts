import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

import { restoreStubs } from "./stubs";

// Vitest runs without globals here, so Testing Library cannot register its own
// cleanup. Unmount every rendered tree after each test, and put back any
// fetch or EventSource stub a test installed.
afterEach(() => {
  cleanup();
  restoreStubs();
});
