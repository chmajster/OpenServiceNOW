import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const apiPort = process.env.OPENSNOW_API_PORT ?? "8080";
if (!/^[1-9][0-9]{0,4}$/.test(apiPort) || Number(apiPort) > 65535) {
  throw new Error("OPENSNOW_API_PORT must be an integer from 1 to 65535");
}
const apiTarget = `http://127.0.0.1:${apiPort}`;

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    host: "127.0.0.1",
    port: 5173,
    proxy: {
      "/api": {
        target: apiTarget,
        changeOrigin: false
      },
      "/health": {
        target: apiTarget,
        changeOrigin: false
      }
    }
  },
  test: {
    include: ["src/**/*.test.{ts,tsx}"],
    environment: "jsdom",
    restoreMocks: true
  }
});
