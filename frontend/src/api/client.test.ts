import { afterEach, expect, test, vi } from "vitest";

import {
  ApiError,
  getAnalyticalSession,
  listAnalyticalSessions,
  logout,
  resetEmployeePassword,
  revokeEmployeeSessions,
  updateEmployee,
} from "./client";

const originalFetch = globalThis.fetch;

afterEach(() => {
  vi.stubGlobal("fetch", originalFetch);
});

test("sends session pagination and employee administration through the public API", async () => {
  const requests: Request[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const request = input instanceof Request ? input : new Request(input, init);
      requests.push(request);
      if (request.url.includes("reset-password")) return jsonResponse(temporaryCredential());
      if (request.method === "PATCH") return jsonResponse(employee());
      if (request.url.includes("/sessions/")) return jsonResponse(sessionDetail());
      return jsonResponse({ items: [], nextCursor: "next-page" });
    }),
  );

  await listAnalyticalSessions("cursor-value");
  await getAnalyticalSession("11111111-1111-4111-8111-111111111111");
  await updateEmployee({ id: employee().id, displayName: "Новое имя", role: "employee" });
  await resetEmployeePassword(employee().id);

  expect(requests[0]?.url).toContain("cursor=cursor-value");
  expect(requests[1]?.url).toContain("/sessions/11111111-1111-4111-8111-111111111111");
  expect(await requests[2]?.json()).toEqual({ displayName: "Новое имя", role: "employee" });
  expect(requests[3]?.url).toContain("reset-password");
});

test("accepts empty successful responses for logout and session revocation", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.resolve(new Response(null, { status: 204 }))),
  );

  await expect(revokeEmployeeSessions(employee().id)).resolves.toBeUndefined();
  await expect(logout()).resolves.toBeUndefined();
});

test("turns malformed API failures into a stable client error", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.resolve(jsonResponse({}, 503))),
  );

  try {
    await logout();
    expect.unreachable("logout should reject a malformed failure response");
  } catch (error) {
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ message: "Сервис временно недоступен", status: 503 });
  }
});

function jsonResponse(body: unknown, status = 200) {
  return Promise.resolve(
    new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" }, status }),
  );
}

function employee() {
  return {
    id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
    login: "ivan.petrov",
    displayName: "Иван Петров",
    role: "employee",
    state: "active",
    createdAt: "2026-08-30T12:00:00Z",
    updatedAt: "2026-08-30T12:00:00Z",
  };
}

function temporaryCredential() {
  return {
    employee: employee(),
    temporaryPassword: "generated-temporary-password",
    expiresAt: "2026-08-31T12:00:00Z",
  };
}

function sessionDetail() {
  return {
    id: "11111111-1111-4111-8111-111111111111",
    name: "Москва - ЖК Северный",
    subtitle: "Первичная недвижимость",
    status: "complete",
    createdAt: "2026-08-30T12:00:00Z",
    createdBy: { id: employee().id, displayName: "Иван Петров" },
    filters: {
      regions: ["Москва"],
      residentialComplexes: ["ЖК Северный"],
      segments: ["Первичная недвижимость"],
      period: { from: "2026-01-01", to: "2026-06-30" },
    },
  };
}
