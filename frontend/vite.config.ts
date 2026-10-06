import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  build: {
    chunkSizeWarningLimit: 900,
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            {
              name: "react",
              test: /node_modules[\\/](react|react-dom|scheduler)[\\/]/,
              priority: 30,
            },
            {
              name: "ant-design",
              test: /node_modules[\\/](@ant-design|antd|rc-)/,
              priority: 20,
            },
            {
              name: "tanstack",
              test: /node_modules[\\/]@tanstack/,
              priority: 10,
            },
            {
              name: "vendor",
              test: /node_modules/,
            },
          ],
        },
      },
    },
  },
  plugins: [react()],
  server: {
    host: "0.0.0.0",
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": "http://localhost:8088",
      "/health": "http://localhost:8088",
    },
  },
});
