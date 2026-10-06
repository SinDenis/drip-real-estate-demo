import "@testing-library/jest-dom/vitest";
import { vi } from "vitest";

const storage = new Map<string, string>();

vi.stubGlobal("localStorage", {
  clear: () => {
    storage.clear();
  },
  getItem: (key: string) => storage.get(key) ?? null,
  removeItem: (key: string) => storage.delete(key),
  setItem: (key: string, value: string) => storage.set(key, value),
});

vi.stubGlobal("matchMedia", (query: string) => ({
  addEventListener: vi.fn(),
  matches: false,
  media: query,
  removeEventListener: vi.fn(),
}));

Object.defineProperty(navigator, "clipboard", {
  configurable: true,
  value: { writeText: vi.fn() },
});

vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
);

window.scrollTo = vi.fn();
