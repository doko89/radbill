import { defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [tailwindcss(), react()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": "http://localhost:3000",
    },
  },
  build: {
    outDir: "dist",
    rollupOptions: {
      onwarn(warning, warn) {
        // Zod v4 ships prose comments mentioning @__PURE__ that Rollup
        // cannot interpret as annotations; it strips them safely. Silence
        // only that noise — every other warning still surfaces.
        if (warning.code === "INVALID_ANNOTATION" && warning.id?.includes("node_modules/zod/")) {
          return;
        }
        warn(warning);
      },
    },
  },
});
