/* eslint-disable max-lines -- Public route journeys stay in one shared authenticated fixture. */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import { createAppRouter } from "./router";

const administrator = {
  id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
  login: "admin",
  displayName: "Аналитик-администратор",
  role: "analytics_admin",
  state: "active",
  requiresPasswordChange: false,
};
const originalFetch = globalThis.fetch;
const originalMatchMedia = globalThis.matchMedia;

afterEach(() => {
  cleanup();
  vi.stubGlobal("fetch", originalFetch);
  vi.stubGlobal("matchMedia", originalMatchMedia);
  localStorage.clear();
});

test("shows one generic error for invalid credentials", async () => {
  await renderRoute("/sessions", ({ path, method }) => {
    if (path === "/api/v1/me") return errorResponse(401, "unauthenticated");
    if (path === "/api/v1/auth/login" && method === "POST") {
      return errorResponse(401, "unauthenticated");
    }
    return errorResponse(500, "unexpected");
  });

  fireEvent.change(await screen.findByLabelText("Имя входа"), { target: { value: "unknown" } });
  fireEvent.change(screen.getByLabelText("Пароль"), { target: { value: "incorrect" } });
  fireEvent.click(screen.getByRole("button", { name: "Войти" }));

  expect(await screen.findByRole("alert")).toHaveTextContent("Имя входа или пароль неверны");
});

test("opens the workspace after a successful login", async () => {
  await renderRoute("/login", ({ path }) => {
    if (path === "/api/v1/me") return errorResponse(401, "unauthenticated");
    if (path === "/api/v1/auth/login") return jsonResponse(administrator);
    if (path === "/api/v1/sessions") return jsonResponse({ items: [] });
    return errorResponse(500, "unexpected");
  });

  fireEvent.change(await screen.findByLabelText("Имя входа"), { target: { value: "admin" } });
  fireEvent.change(screen.getByLabelText("Пароль"), { target: { value: "temporary-password" } });
  fireEvent.click(screen.getByRole("button", { name: "Войти" }));

  expect(await screen.findByText("Выберите аналитическую сессию")).toBeVisible();
});

test("selects a supported current market without an analysis period", async () => {
  await renderRoute("/sessions/new", ({ path }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions") return jsonResponse({ items: [] });
    if (path === "/api/v1/catalog/regions") {
      return jsonResponse({
        version: "fns-2025-10-01",
        items: [
          { code: "77", name: "Москва" },
          { code: "78", name: "Санкт-Петербург" },
        ],
      });
    }
    if (path === "/api/v1/catalog/segments") {
      return jsonResponse({
        items: [
          { code: "residential", name: "Жилые помещения", eiszhsSupported: true },
          { code: "apartments", name: "Апартаменты", eiszhsSupported: false },
        ],
      });
    }
    if (path === "/api/v1/catalog/regions/77/complexes") {
      return jsonResponse({
        freshness: "fresh",
        observedAt: "2026-08-31T12:00:00Z",
        expiresAt: "2026-09-01T12:00:00Z",
        items: [{ complexId: "1001", name: "ЖК Северный" }],
      });
    }
    return errorResponse(500, "unexpected");
  });

  fireEvent.mouseDown(await screen.findByLabelText("Регионы"));
  fireEvent.click(await screen.findByText("Москва"));
  fireEvent.mouseDown(await screen.findByLabelText("Жилые комплексы"));
  expect(await screen.findByText("ЖК Северный")).toBeInTheDocument();
  expect(screen.queryByText("Период анализа")).not.toBeInTheDocument();
  fireEvent.mouseDown(screen.getByLabelText("Сегменты недвижимости"));
  expect(screen.getByText("Не поддерживается ЕИСЖС")).toBeInTheDocument();
});

test("creates a fresh session and opens its persisted dashboard", async () => {
  let idempotencyKey = "";
  const created = {
    ...sessionDetail("complete", "01996c43-6c00-7abc-8def-1234567890ab"),
    observedWindow: { from: "2026-08-31T10:00:00Z", to: "2026-08-31T11:00:00Z" },
  };
  // eslint-disable-next-line complexity -- One route fixture records the POST and serves its redirect target.
  await renderRoute("/sessions/new", ({ path, method, request }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions" && method === "GET") return jsonResponse({ items: [] });
    if (path === "/api/v1/sessions" && method === "POST") {
      idempotencyKey = request.headers.get("Idempotency-Key") ?? "";
      return jsonResponse(created, 201);
    }
    if (path === `/api/v1/sessions/${created.id}`) return jsonResponse(created);
    if (path === "/api/v1/catalog/regions") {
      return jsonResponse({
        version: "fns-2025-10-01",
        source: "https://www.nalog.gov.ru/",
        items: [{ code: "77", name: "Москва" }],
      });
    }
    if (path === "/api/v1/catalog/segments") {
      return jsonResponse({
        items: [{ code: "residential", name: "Жилые помещения", eiszhsSupported: true }],
      });
    }
    if (path === "/api/v1/catalog/regions/77/complexes") {
      return jsonResponse({
        freshness: "fresh",
        observedAt: "2026-08-31T12:00:00Z",
        expiresAt: "2026-09-01T12:00:00Z",
        items: [{ complexId: "1001", name: "ЖК Северный" }],
      });
    }
    return errorResponse(500, "unexpected");
  });

  fireEvent.mouseDown(await screen.findByLabelText("Регионы"));
  fireEvent.click(await screen.findByText("Москва"));
  fireEvent.mouseDown(await screen.findByLabelText("Жилые комплексы"));
  fireEvent.click(await screen.findByText("ЖК Северный"));
  fireEvent.mouseDown(screen.getByLabelText("Сегменты недвижимости"));
  fireEvent.click(await screen.findByText("Жилые помещения"));
  fireEvent.click(screen.getByRole("button", { name: "Создать сессию" }));

  expect(await screen.findByRole("heading", { name: "Москва - ЖК Северный" })).toBeVisible();
  expect(screen.getByText("Фактическое окно наблюдения")).toBeVisible();
  expect(idempotencyKey).toHaveLength(32);
});

test("reuses one idempotency key for a failed logical submission and rotates after intent changes", async () => {
  const keys: string[] = [];
  const created = sessionDetail("complete", "01996c43-6c00-7abc-8def-1234567890ab");
  // eslint-disable-next-line complexity -- The fixture keeps one complete logical submission journey visible.
  await renderRoute("/sessions/new", ({ path, method, request }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions" && method === "GET") return jsonResponse({ items: [] });
    if (path === "/api/v1/sessions" && method === "POST") {
      keys.push(request.headers.get("Idempotency-Key") ?? "");
      return keys.length < 3 ? errorResponse(503, "unavailable") : jsonResponse(created, 201);
    }
    if (path === `/api/v1/sessions/${created.id}`) return jsonResponse(created);
    if (path === "/api/v1/catalog/regions") {
      return jsonResponse({
        version: "fns-2025-10-01",
        source: "https://www.nalog.gov.ru/",
        items: [{ code: "77", name: "Москва" }],
      });
    }
    if (path === "/api/v1/catalog/segments") {
      return jsonResponse({
        items: [
          { code: "residential", name: "Жилые помещения", eiszhsSupported: true },
          { code: "apartments", name: "Апартаменты", eiszhsSupported: false },
        ],
      });
    }
    if (path === "/api/v1/catalog/regions/77/complexes") {
      return jsonResponse({
        freshness: "fresh",
        observedAt: "2026-08-31T12:00:00Z",
        expiresAt: "2026-09-01T12:00:00Z",
        items: [{ complexId: "1001", name: "ЖК Северный" }],
      });
    }
    return errorResponse(500, "unexpected");
  });

  fireEvent.mouseDown(await screen.findByLabelText("Регионы"));
  fireEvent.click(await screen.findByText("Москва"));
  fireEvent.mouseDown(await screen.findByLabelText("Жилые комплексы"));
  fireEvent.click(await screen.findByText("ЖК Северный"));
  fireEvent.mouseDown(screen.getByLabelText("Сегменты недвижимости"));
  fireEvent.click(await screen.findByText("Жилые помещения"));
  fireEvent.click(screen.getByRole("button", { name: "Создать сессию" }));
  await waitFor(() => {
    expect(keys).toHaveLength(1);
  });
  fireEvent.click(screen.getByRole("button", { name: "Создать сессию" }));
  await waitFor(() => {
    expect(keys).toHaveLength(2);
  });
  expect(keys[1]).toBe(keys[0]);

  fireEvent.mouseDown(screen.getByLabelText("Сегменты недвижимости"));
  fireEvent.click(await screen.findByText("Апартаменты"));
  fireEvent.click(screen.getByRole("button", { name: "Создать сессию" }));
  expect(await screen.findByRole("heading", { name: "Москва - ЖК Северный" })).toBeVisible();
  expect(keys[2]).not.toBe(keys[1]);
});

test("renders a collecting session without invented observation dates or a period filter", async () => {
  const persisted = sessionDetail("collecting", "01996c43-6c00-7abc-8def-1234567890ab");
  const cold = {
    id: persisted.id,
    name: persisted.name,
    subtitle: "Жилые помещения",
    status: persisted.status,
    createdAt: persisted.createdAt,
    createdBy: persisted.createdBy,
    filters: {
      regions: persisted.filters.regions,
      residentialComplexes: persisted.filters.residentialComplexes,
      segments: persisted.filters.segments,
    },
  };
  await renderRoute(`/sessions/${cold.id}`, ({ path }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions") return jsonResponse({ items: [cold] });
    if (path === `/api/v1/sessions/${cold.id}`) return jsonResponse(cold);
    return errorResponse(500, "unexpected");
  });

  expect(await screen.findByText("Данные текущего месяца еще не получены")).toBeVisible();
  expect(screen.queryByText("Период анализа")).not.toBeInTheDocument();
  expect(screen.queryByText(/01\.01\.1970|01\.01\.0001/)).not.toBeInTheDocument();
});

test("advances a cold current snapshot sequentially and renders its ЕИСЖС dashboard", async () => {
  const id = "01996c43-6c00-7abc-8def-1234567890ab";
  const cold = {
    ...sessionDetail("collecting", id),
    observedWindow: undefined,
    collectionProgress: collectionProgressFixture(0, 1),
  };
  let collectCalls = 0;
  await renderRoute(`/sessions/${id}`, ({ path, method }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions") return jsonResponse({ items: [cold] });
    if (path === `/api/v1/sessions/${id}` && method === "GET") return jsonResponse(cold);
    if (path === `/api/v1/sessions/${id}/collect` && method === "POST") {
      collectCalls += 1;
      if (collectCalls === 1) {
        return jsonResponse({
          ...cold,
          collectionProgress: collectionProgressFixture(1, 2),
        });
      }
      return jsonResponse({
        ...cold,
        status: "complete",
        collectionProgress: collectionProgressFixture(2, 2, 1, "complete"),
        observedWindow: { from: "2026-08-31T13:06:00Z", to: "2026-08-31T13:06:00Z" },
        currentEiszhsDashboard: {
          sourceLabel: "Текущий срез ЕИСЖС",
          cumulativeDduCount: 17,
        },
      });
    }
    return errorResponse(500, "unexpected");
  });

  expect(await screen.findByText("Текущий срез ЕИСЖС")).toBeVisible();
  expect(screen.getByText("17")).toBeVisible();
  expect(screen.getByText("2 из 2 запросов ЕИСЖС")).toBeVisible();
  expect(collectCalls).toBe(2);
});

test("shows an unavailable DDU metric without inventing a zero", async () => {
  const session = {
    ...sessionDetail("complete", "01996c43-6c00-7abc-8def-1234567890ad"),
    currentEiszhsDashboard: { sourceLabel: "Текущий срез ЕИСЖС" },
  };
  await renderRoute(`/sessions/${session.id}`, ({ path }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions") return jsonResponse({ items: [session] });
    if (path === `/api/v1/sessions/${session.id}`) return jsonResponse(session);
    return errorResponse(500, "unexpected");
  });

  expect(await screen.findByText("Накопительное количество ДДУ")).toBeVisible();
  expect(screen.getByText("Нет данных", { exact: true })).toBeVisible();
  expect(screen.queryByText("0", { exact: true })).not.toBeInTheDocument();
});

test("shows resumable object-level collection progress", async () => {
  const id = "01996c43-6c00-7abc-8def-1234567890aa";
  const collecting = {
    ...sessionDetail("collecting", id),
    observedWindow: undefined,
    collectionProgress: {
      phase: "collecting_objects" as const,
      completedRequests: 3,
      totalRequests: 6,
      discoveredTotal: 3,
      cached: 0,
      fetched: 0,
      failed: 0,
      remaining: 3,
      currentObjectId: "9001",
      nextActionAt: "2026-08-31T13:06:00Z",
      percent: 50,
    },
  };
  await renderRoute(`/sessions/${id}`, ({ path, method }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions") return jsonResponse({ items: [collecting] });
    if (path === `/api/v1/sessions/${id}` && method === "GET") return jsonResponse(collecting);
    if (path === `/api/v1/sessions/${id}/collect` && method === "POST") {
      return errorResponse(503, "paused fixture");
    }
    return errorResponse(500, "unexpected");
  });

  expect(await screen.findByText("3 из 6 запросов ЕИСЖС")).toBeVisible();
  expect(screen.getByText("Найдено объектов: 3")).toBeVisible();
  expect(screen.getByText("Осталось запросов: 3")).toBeVisible();
  expect(screen.getByText("Текущий объект ЕИСЖС: 9001")).toBeVisible();
  expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "50");
});

test("shows planning progress without inventing a request total or percentage", async () => {
  const id = "01996c43-6c00-7abc-8def-1234567890ac";
  const cold = {
    ...sessionDetail("collecting", id),
    observedWindow: undefined,
    collectionProgress: {
      phase: "discovering_objects" as const,
      completedRequests: 0,
      discoveredTotal: 0,
      cached: 0,
      fetched: 0,
      failed: 0,
      currentComplexId: "1001",
      nextActionAt: "2026-08-31T13:06:00Z",
    },
  };
  await renderRoute(`/sessions/${id}`, ({ path, method }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions") return jsonResponse({ items: [cold] });
    if (path === `/api/v1/sessions/${id}` && method === "GET") return jsonResponse(cold);
    if (path === `/api/v1/sessions/${id}/collect` && method === "POST") {
      return errorResponse(503, "planning fixture paused");
    }
    return errorResponse(500, "unexpected");
  });

  expect(await screen.findByText("0 запросов ЕИСЖС - выполняется планирование")).toBeVisible();
  expect(screen.queryByRole("progressbar")).not.toBeInTheDocument();
});

test("retries an unavailable complex catalog and shows a stale warning", async () => {
  let complexCalls = 0;
  await renderRoute("/sessions/new", ({ path }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions") return jsonResponse({ items: [] });
    if (path === "/api/v1/catalog/regions") {
      return jsonResponse({
        version: "fns-2025-10-01",
        source: "https://www.nalog.gov.ru/",
        items: [{ code: "77", name: "Москва" }],
      });
    }
    if (path === "/api/v1/catalog/segments") {
      return jsonResponse({
        items: [{ code: "residential", name: "Жилые помещения", eiszhsSupported: true }],
      });
    }
    if (path === "/api/v1/catalog/regions/77/complexes") {
      complexCalls += 1;
      if (complexCalls === 1) return errorResponse(503, "unavailable");
      return jsonResponse({
        freshness: "stale",
        observedAt: "2026-08-29T12:00:00Z",
        expiresAt: "2026-08-30T12:00:00Z",
        warning: "Показан устаревший справочник ЖК",
        items: [{ complexId: "1001", name: "ЖК Северный" }],
      });
    }
    return errorResponse(500, "unexpected");
  });

  fireEvent.mouseDown(await screen.findByLabelText("Регионы"));
  fireEvent.click(await screen.findByText("Москва"));
  expect(await screen.findByRole("alert")).toHaveTextContent("Справочник ЖК временно недоступен");
  fireEvent.click(screen.getByRole("button", { name: "Повторить" }));
  expect(await screen.findByRole("status")).toHaveTextContent("устаревший справочник ЖК");
});

test("blocks an unsupported-only segment set and warns for mixed support", async () => {
  await renderRoute("/sessions/new", ({ path }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions") return jsonResponse({ items: [] });
    if (path === "/api/v1/catalog/regions") {
      return jsonResponse({
        version: "fns-2025-10-01",
        source: "https://www.nalog.gov.ru/",
        items: [{ code: "77", name: "Москва" }],
      });
    }
    if (path === "/api/v1/catalog/segments") {
      return jsonResponse({
        items: [
          { code: "residential", name: "Жилые помещения", eiszhsSupported: true },
          { code: "apartments", name: "Апартаменты", eiszhsSupported: false },
        ],
      });
    }
    return errorResponse(500, "unexpected");
  });

  const segments = await screen.findByLabelText("Сегменты недвижимости");
  fireEvent.mouseDown(segments);
  fireEvent.click(await screen.findByText("Апартаменты"));
  expect(await screen.findByRole("alert")).toHaveTextContent("не поддерживаются ЕИСЖС");
  fireEvent.mouseDown(segments);
  fireEvent.click(await screen.findByText("Жилые помещения"));
  expect(await screen.findByRole("status")).toHaveTextContent("Часть сегментов");
});

test("activates a pending account and opens the empty workspace", async () => {
  let activated = false;
  await renderRoute("/sessions", ({ path }) => {
    if (path === "/api/v1/me")
      return jsonResponse({
        ...administrator,
        state: "pending_activation",
        requiresPasswordChange: true,
      });
    if (path === "/api/v1/auth/change-password") {
      activated = true;
      return jsonResponse(administrator);
    }
    if (path === "/api/v1/sessions") return jsonResponse({ items: [] });
    return errorResponse(500, "unexpected");
  });

  fireEvent.change(await screen.findByLabelText("Новый пароль", { exact: true }), {
    target: { value: "A durable password phrase" },
  });
  fireEvent.change(screen.getByLabelText("Повторите новый пароль"), {
    target: { value: "A durable password phrase" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Сохранить пароль" }));

  expect(await screen.findByText("Пока нет аналитических сессий")).toBeVisible();
  expect(activated).toBe(true);
});

test("validates a new password and reports a rejected password", async () => {
  await renderRoute("/sessions", ({ path }) => {
    if (path === "/api/v1/me") {
      return jsonResponse({ ...administrator, requiresPasswordChange: true });
    }
    if (path === "/api/v1/auth/change-password") return errorResponse(422, "weak_password");
    return errorResponse(500, "unexpected");
  });
  const password = await screen.findByLabelText("Новый пароль", { exact: true });
  const confirmation = screen.getByLabelText("Повторите новый пароль");
  fireEvent.change(password, { target: { value: "A durable password phrase" } });
  fireEvent.change(confirmation, { target: { value: "Different durable phrase" } });
  fireEvent.click(screen.getByRole("button", { name: "Сохранить пароль" }));
  expect(await screen.findByText("Пароли не совпадают")).toBeInTheDocument();

  fireEvent.change(confirmation, { target: { value: "A durable password phrase" } });
  await waitFor(() => {
    expect(confirmation.closest(".ant-input-affix-wrapper")).toHaveClass(
      "ant-input-status-success",
    );
  });
  fireEvent.click(screen.getByRole("button", { name: "Сохранить пароль" }));
  expect(await screen.findByText("Пароль не соответствует требованиям")).toBeVisible();
});

test("opens a selected analytical session and remembers a collapsed sidebar", async () => {
  await renderRoute("/sessions/11111111-1111-4111-8111-111111111111", sessionResponder);

  expect(await screen.findByRole("heading", { name: "Москва - ЖК Северный" })).toBeVisible();
  expect(screen.getByText("Канонические фильтры")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Свернуть боковую панель" }));
  expect(localStorage.getItem("sidebar-collapsed")).toBe("true");
});

test("paginates grouped sessions and opens a recent session", async () => {
  let page = 0;
  await renderRoute("/sessions", ({ path }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions") {
      page += 1;
      return page === 1
        ? jsonResponse({
            items: [sessionSummary()],
            nextCursor: "next-page",
          })
        : jsonResponse({
            items: [sessionSummary("complete", "22222222-2222-4222-8222-222222222222")],
          });
    }
    if (path.includes("/api/v1/sessions/")) {
      return jsonResponse(sessionDetail("complete", "22222222-2222-4222-8222-222222222222"));
    }
    return errorResponse(500, "unexpected");
  });

  fireEvent.click(await screen.findByRole("button", { name: "Показать еще" }));
  const recent = await screen.findByRole("region", { name: "Недавние" });
  fireEvent.click(within(recent).getByRole("button"));

  expect(await screen.findByText("Канонические фильтры")).toBeVisible();
});

test("uses a drawer for selecting sessions on a narrow screen", async () => {
  vi.stubGlobal("matchMedia", () => mediaQuery(true));
  await renderRoute("/sessions", sessionResponder);

  fireEvent.click(
    await screen.findByRole("button", { name: "Открыть список аналитических сессий" }),
  );
  fireEvent.click(await screen.findByRole("button", { name: /Москва - ЖК Северный/ }));

  expect(await screen.findByRole("heading", { name: "Москва - ЖК Северный" })).toBeVisible();
});

test("shows the same public state for a missing session", async () => {
  await renderRoute("/sessions/missing", ({ path }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions") return jsonResponse({ items: [] });
    return errorResponse(404, "not_found");
  });
  expect(await screen.findByText("Сессия не найдена")).toBeVisible();
});

test("distinguishes a session service failure from a missing session", async () => {
  await renderRoute("/sessions/unavailable", ({ path }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions") return jsonResponse({ items: [] });
    return errorResponse(503, "unavailable");
  });
  expect(await screen.findByText("Не удалось открыть аналитическую сессию")).toBeVisible();
});

test("returns to login at the current URL when the server-side session expires", async () => {
  let currentUserRequests = 0;
  await renderRoute("/sessions/11111111-1111-4111-8111-111111111111", ({ path }) => {
    if (path === "/api/v1/me") {
      currentUserRequests += 1;
      return currentUserRequests === 1
        ? jsonResponse(administrator)
        : errorResponse(401, "unauthenticated");
    }
    if (path === "/api/v1/sessions") return jsonResponse({ items: [] });
    if (path.includes("/api/v1/sessions/")) return errorResponse(401, "unauthenticated");
    return errorResponse(500, "unexpected");
  });

  expect(await screen.findByRole("heading", { name: "Вход" })).toBeVisible();
  expect(window.location.pathname).toBe("/sessions/11111111-1111-4111-8111-111111111111");
});

test("creates an employee and reveals the generated password once", async () => {
  const employees = [employeeAccount("admin", "Аналитик-администратор", "analytics_admin")];
  await renderRoute("/employees", employeeResponder(employees));

  fireEvent.click(await screen.findByRole("button", { name: "Добавить сотрудника" }));
  const loginInput = screen.getByLabelText("Имя входа");
  const displayNameInput = screen.getByLabelText("Отображаемое имя");
  fireEvent.change(loginInput, { target: { value: "ivan.petrov" } });
  fireEvent.change(displayNameInput, { target: { value: "Иван Петров" } });
  await waitFor(() => {
    expect(loginInput).toHaveClass("ant-input-status-success");
    expect(displayNameInput).toHaveClass("ant-input-status-success");
  });
  fireEvent.click(screen.getByRole("button", { name: "Создать аккаунт" }));

  const temporaryPassword = await screen.findByRole("textbox", { name: "Временный пароль" });
  expect(temporaryPassword).toHaveValue("generated-temporary-password");
  fireEvent.click(screen.getByRole("button", { name: "Готово" }));
  expect(screen.queryByText("Пароль будет показан только сейчас")).not.toBeInTheDocument();
});

test("shows an accessible service error", async () => {
  await renderRoute("/sessions", () => errorResponse(503, "unavailable"));
  expect(await screen.findByRole("alert")).toHaveTextContent("временно недоступно");
});

async function renderRoute(path: string, responder: Responder) {
  vi.stubGlobal(
    "fetch",
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const request = input instanceof Request ? input : new Request(input, init);
      return Promise.resolve(
        responder({ path: new URL(request.url).pathname, method: request.method, request }),
      );
    }),
  );
  const router = createAppRouter();
  await router.navigate({ to: path });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

type Responder = (request: { path: string; method: string; request: Request }) => Response;

function sessionResponder({ path }: { path: string }) {
  if (path === "/api/v1/me") return jsonResponse(administrator);
  if (path === "/api/v1/sessions") return jsonResponse({ items: [sessionSummary()] });
  if (path.includes("/api/v1/sessions/")) {
    return jsonResponse({
      ...sessionSummary(),
      filters: {
        regions: ["Москва"],
        residentialComplexes: ["ЖК Северный"],
        segments: ["Первичная недвижимость"],
        period: { from: "2026-01-01", to: "2026-06-30" },
      },
      observedWindow: { from: "2026-01-01T00:00:00Z", to: "2026-06-30T00:00:00Z" },
    });
  }
  return errorResponse(500, "unexpected");
}

function employeeResponder(employees: Array<Record<string, unknown>>): Responder {
  return ({ path, method }) => {
    if (path === "/api/v1/me") return jsonResponse(administrator);
    if (path === "/api/v1/sessions") return jsonResponse({ items: [] });
    if (path === "/api/v1/employees" && method === "GET") return jsonResponse({ items: employees });
    if (path === "/api/v1/employees" && method === "POST") {
      const employee = employeeAccount("ivan.petrov", "Иван Петров", "employee");
      employees.push(employee);
      return jsonResponse(credential(employee), 201);
    }
    const employee = employees[1];
    if (path.endsWith("/deactivate")) {
      employee.state = "deactivated";
      return jsonResponse(employee);
    }
    if (path.endsWith("/restore")) {
      employee.state = "pending_activation";
      return jsonResponse(credential(employee));
    }
    return errorResponse(500, "unexpected");
  };
}

function employeeAccount(login: string, displayName: string, role: string) {
  return {
    id: login === "admin" ? administrator.id : "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
    login,
    displayName,
    role,
    state: "active",
    createdAt: "2026-08-30T12:00:00Z",
    updatedAt: "2026-08-30T12:00:00Z",
  };
}

function credential(employee: Record<string, unknown>) {
  return {
    employee,
    temporaryPassword: "generated-temporary-password",
    expiresAt: "2026-08-31T12:00:00Z",
  };
}

function sessionSummary(status = "collecting", id = "11111111-1111-4111-8111-111111111111") {
  return {
    id,
    name: "Москва - ЖК Северный",
    subtitle: "Первичная недвижимость · 01.01.2026 - 30.06.2026",
    status,
    createdAt: "2026-08-30T12:00:00Z",
    createdBy: { id: administrator.id, displayName: administrator.displayName },
  };
}

function collectionProgressFixture(
  completedRequests: number,
  totalRequests: number,
  fetched = 0,
  phase: "collecting_objects" | "complete" = "collecting_objects",
) {
  return {
    phase,
    completedRequests,
    totalRequests,
    discoveredTotal: 1,
    cached: 0,
    fetched,
    failed: 0,
    remaining: totalRequests - completedRequests,
    percent: Math.round((completedRequests / totalRequests) * 100),
  };
}

function sessionDetail(status: string, id: string) {
  return {
    ...sessionSummary(status, id),
    filters: {
      regions: ["Москва"],
      residentialComplexes: ["ЖК Северный"],
      segments: ["Первичная недвижимость"],
      period: { from: "2026-01-01", to: "2026-06-30" },
    },
    observedWindow: { from: "2026-01-01T00:00:00Z", to: "2026-06-30T00:00:00Z" },
  };
}

function mediaQuery(matches: boolean) {
  return {
    addEventListener: vi.fn(),
    matches,
    media: "(max-width: 760px)",
    removeEventListener: vi.fn(),
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json" },
    status,
  });
}

function errorResponse(status: number, code: string) {
  return jsonResponse({ error: { code, message: code } }, status);
}
