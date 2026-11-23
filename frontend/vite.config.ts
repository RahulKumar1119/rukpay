import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// The dev server uses port 5173 and refuses another port. The backend allows the
// browser origin http://localhost:5173 by default, so a different port would make
// every request fail CORS.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: true,
  },
  preview: {
    port: 4173,
    strictPort: true,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
    css: false,
    // Component tests render whole screens and some wait for a real second. A
    // shared CI machine can need more than Vitest's default of 5 seconds.
    testTimeout: 20_000,
  },
});
