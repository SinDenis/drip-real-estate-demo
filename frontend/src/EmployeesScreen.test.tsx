import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { afterEach, expect, test, vi } from "vitest";

import { EmployeesScreen } from "./EmployeesScreen";

const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  vi.stubGlobal("fetch", originalFetch);
});

test(
  "edits an employee, resets the password, copies it, and revokes sessions",
  { timeout: 10_000 },
  async () => {
    const requests: string[] = [];
    const employee = employeeAccount();
    const copyPassword = vi.fn();
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: copyPassword },
    });
    renderScreen(({ path, method }) => {
      requests.push(`${method} ${path}`);
      if (path === "/api/v1/employees") return jsonResponse({ items: [employee] });
      if (method === "PATCH") {
        employee.displayName = "Иван Петров-младший";
        return jsonResponse(employee);
      }
      if (path.endsWith("/reset-password")) return jsonResponse(credential(employee));
      if (path.endsWith("/revoke-sessions")) return new Response(null, { status: 204 });
      return errorResponse(500);
    });

    fireEvent.click((await employeeRow("Иван Петров")).getByRole("button", { name: "Изменить" }));
    const displayName = screen.getByLabelText("Отображаемое имя");
    fireEvent.change(displayName, { target: { value: "Иван Петров-младший" } });
    await waitFor(() => {
      expect(displayName).toHaveClass("ant-input-status-success");
    });
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));

    const updatedRow = await employeeRow("Иван Петров-младший");
    fireEvent.click(updatedRow.getByRole("button", { name: "Новый пароль" }));
    fireEvent.click(await screen.findByRole("button", { name: "Копировать временный пароль" }));
    expect(copyPassword).toHaveBeenCalledWith("generated-temporary-password");
    fireEvent.click(screen.getByRole("button", { name: "Готово" }));
    fireEvent.click(updatedRow.getByRole("button", { name: "Завершить входы" }));

    await waitFor(() => {
      expect(requests.some((request) => request.endsWith("/revoke-sessions"))).toBe(true);
    });
  },
);

test("deactivates and restores an employee from the public screen", async () => {
  const employee = employeeAccount();
  renderScreen(({ path }) => {
    if (path === "/api/v1/employees") return jsonResponse({ items: [employee] });
    if (path.endsWith("/deactivate")) {
      employee.state = "deactivated";
      return jsonResponse(employee);
    }
    if (path.endsWith("/restore")) {
      employee.state = "pending_activation";
      return jsonResponse(credential(employee));
    }
    return errorResponse(500);
  });
  const row = await employeeRow("Иван Петров");

  fireEvent.click(row.getByRole("button", { name: "Деактивировать" }));
  expect(await row.findByText("Деактивирована")).toBeVisible();
  fireEvent.click(row.getByRole("button", { name: "Восстановить" }));

  expect(await screen.findByText("Пароль будет показан только сейчас")).toBeInTheDocument();
});

test("reports an employee list failure", async () => {
  renderScreen(() => errorResponse(503));
  expect(await screen.findByText("Не удалось загрузить сотрудников")).toBeInTheDocument();
});

test.each([
  [409, "Нельзя изменить последнего активного администратора"],
  [500, "Не удалось изменить доступ сотрудника"],
])("reports an access mutation failure with status %s", async (status, message) => {
  renderScreen(({ path }) =>
    path === "/api/v1/employees"
      ? jsonResponse({ items: [employeeAccount()] })
      : errorResponse(status),
  );

  fireEvent.click(
    (await employeeRow("Иван Петров")).getByRole("button", { name: "Деактивировать" }),
  );
  expect(await screen.findByText(message)).toBeInTheDocument();
});

function renderScreen(responder: Responder) {
  vi.stubGlobal(
    "fetch",
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const request = input instanceof Request ? input : new Request(input, init);
      return Promise.resolve(
        responder({ path: new URL(request.url).pathname, method: request.method }),
      );
    }),
  );
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <QueryClientProvider client={queryClient}>
        <EmployeesScreen />
      </QueryClientProvider>
    </ConfigProvider>,
  );
}

async function employeeRow(name: string) {
  const row = (await screen.findByText(name)).closest("tr");
  if (!row) throw new Error("Employee row is missing");
  return within(row);
}

type Responder = (request: { path: string; method: string }) => Response;

function employeeAccount() {
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

function credential(employee: ReturnType<typeof employeeAccount>) {
  return {
    employee,
    temporaryPassword: "generated-temporary-password",
    expiresAt: "2026-08-31T12:00:00Z",
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json" },
    status,
  });
}

function errorResponse(status: number) {
  return jsonResponse({ error: { code: "failure", message: "failure" } }, status);
}
