import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { ConfigProvider } from "antd";

import { router } from "./router";
import "./styles.css";

const root = document.getElementById("root");
if (!root) {
  throw new Error("React root element is missing");
}

const queryClient = new QueryClient({
  defaultOptions: { queries: { refetchOnWindowFocus: false } },
});

createRoot(root).render(
  <StrictMode>
    <ConfigProvider
      theme={{
        token: {
          colorPrimary: "#486f8d",
          colorBgLayout: "#f2f6fa",
          colorBorder: "#d9e5ee",
          borderRadius: 16,
          borderRadiusLG: 24,
          colorText: "#213547",
          colorTextSecondary: "#61778a",
          fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif",
        },
        components: {
          Button: { borderRadius: 12, borderRadiusLG: 14, borderRadiusSM: 10 },
          Input: { borderRadius: 12, borderRadiusLG: 14 },
          Select: { borderRadius: 12, borderRadiusLG: 14 },
          Card: { borderRadiusLG: 24 },
          Modal: { borderRadiusLG: 24 },
          Table: { headerBg: "#eef4f8", borderColor: "#d9e5ee", headerBorderRadius: 14 },
        },
      }}
    >
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </ConfigProvider>
  </StrictMode>,
);
