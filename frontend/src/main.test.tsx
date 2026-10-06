import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

const originalFetch = globalThis.fetch;

beforeEach(() => {
  vi.resetModules();
});

afterEach(() => {
  cleanup();
  vi.stubGlobal("fetch", originalFetch);
});

test("fails fast when the public HTML shell has no React root", async () => {
  document.body.innerHTML = "";

  await expect(import("./main")).rejects.toThrow("React root element is missing");
});

test("mounts the application into the public HTML shell", async () => {
  document.body.innerHTML = '<div id="root"></div>';
  vi.stubGlobal(
    "fetch",
    vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({ error: { code: "unauthenticated", message: "unauthenticated" } }),
          {
            headers: { "Content-Type": "application/json" },
            status: 401,
          },
        ),
      ),
    ),
  );

  await import("./main");

  expect(await screen.findByRole("heading", { name: "Вход" })).toBeVisible();
});
