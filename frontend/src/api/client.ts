import createClient from "openapi-fetch";

import type { components, paths } from "./schema";

export type CurrentUser = components["schemas"]["CurrentUser"];
export type AnalyticalSessionPage = components["schemas"]["AnalyticalSessionPage"];
export type AnalyticalSessionDetail = components["schemas"]["AnalyticalSessionDetail"];
export type Employee = components["schemas"]["Employee"];
export type EmployeePage = components["schemas"]["EmployeePage"];
export type TemporaryCredential = components["schemas"]["TemporaryCredential"];
export type Role = components["schemas"]["Role"];
export type RegionCatalog = components["schemas"]["RegionCatalog"];
export type SegmentCatalog = components["schemas"]["SegmentCatalog"];
export type ResidentialComplexCatalog = components["schemas"]["ResidentialComplexCatalog"];
export type CreateAnalyticalSessionRequest =
  components["schemas"]["CreateAnalyticalSessionRequest"];

const sameOriginFetch: typeof fetch = (input, init) => globalThis.fetch(input, init);
const client = createClient<paths>({
  baseUrl: window.location.origin,
  credentials: "include",
  fetch: sameOriginFetch,
});
export const AUTHENTICATION_REQUIRED_EVENT = "drip-real-estate:authentication-required";

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export async function getCurrentUser(): Promise<CurrentUser> {
  return unwrap(await client.GET("/api/v1/me"));
}

export async function login(credentials: {
  login: string;
  password: string;
}): Promise<CurrentUser> {
  return unwrap(await client.POST("/api/v1/auth/login", { body: credentials }));
}

export async function changeTemporaryPassword(password: string): Promise<CurrentUser> {
  return unwrapAuthenticated(
    await client.POST("/api/v1/auth/change-password", { body: { password } }),
  );
}

export async function listAnalyticalSessions(cursor?: string): Promise<AnalyticalSessionPage> {
  return unwrapAuthenticated(
    await client.GET("/api/v1/sessions", {
      params: { query: { cursor, limit: 30 } },
    }),
  );
}

export async function getAnalyticalSession(id: string): Promise<AnalyticalSessionDetail> {
  return unwrapAuthenticated(
    await client.GET("/api/v1/sessions/{sessionId}", { params: { path: { sessionId: id } } }),
  );
}

export async function collectCurrentSnapshot(id: string): Promise<AnalyticalSessionDetail> {
  return unwrapAuthenticated(
    await client.POST("/api/v1/sessions/{sessionId}/collect", {
      params: { path: { sessionId: id } },
    }),
  );
}

export async function createAnalyticalSession(
  input: CreateAnalyticalSessionRequest,
  idempotencyKey: string,
): Promise<AnalyticalSessionDetail> {
  return unwrapAuthenticated(
    await client.POST("/api/v1/sessions", {
      body: input,
      params: { header: { "Idempotency-Key": idempotencyKey } },
    }),
  );
}

export async function listRegions(): Promise<RegionCatalog> {
  return unwrapAuthenticated(await client.GET("/api/v1/catalog/regions"));
}

export async function listSegments(): Promise<SegmentCatalog> {
  return unwrapAuthenticated(await client.GET("/api/v1/catalog/segments"));
}

export async function listResidentialComplexes(
  regionCode: string,
): Promise<ResidentialComplexCatalog> {
  return unwrapAuthenticated(
    await client.GET("/api/v1/catalog/regions/{regionCode}/complexes", {
      params: { path: { regionCode } },
    }),
  );
}

export async function listEmployees(): Promise<EmployeePage> {
  return unwrapAuthenticated(await client.GET("/api/v1/employees"));
}

export async function createEmployee(input: {
  login: string;
  displayName: string;
  role: Role;
}): Promise<TemporaryCredential> {
  return unwrapAuthenticated(await client.POST("/api/v1/employees", { body: input }));
}

export async function updateEmployee(input: {
  id: string;
  displayName?: string;
  role?: Role;
}): Promise<Employee> {
  return unwrapAuthenticated(
    await client.PATCH("/api/v1/employees/{employeeId}", {
      params: { path: { employeeId: input.id } },
      body: { displayName: input.displayName, role: input.role },
    }),
  );
}

export async function deactivateEmployee(id: string): Promise<Employee> {
  return unwrapAuthenticated(
    await client.POST("/api/v1/employees/{employeeId}/deactivate", {
      params: { path: { employeeId: id } },
    }),
  );
}

export async function restoreEmployee(id: string): Promise<TemporaryCredential> {
  return unwrapAuthenticated(
    await client.POST("/api/v1/employees/{employeeId}/restore", {
      params: { path: { employeeId: id } },
    }),
  );
}

export async function resetEmployeePassword(id: string): Promise<TemporaryCredential> {
  return unwrapAuthenticated(
    await client.POST("/api/v1/employees/{employeeId}/reset-password", {
      params: { path: { employeeId: id } },
    }),
  );
}

export async function revokeEmployeeSessions(id: string): Promise<void> {
  const result = await client.POST("/api/v1/employees/{employeeId}/revoke-sessions", {
    params: { path: { employeeId: id } },
  });
  unwrapVoidAuthenticated(result);
}

export async function logout(): Promise<void> {
  const result = await client.POST("/api/v1/auth/logout");
  unwrapVoid(result);
}

type ApiResult<T> = {
  data?: T;
  error?: components["schemas"]["ErrorResponse"];
  response: Response;
};

function unwrap<T>(result: ApiResult<T>): T {
  if (result.data !== undefined) {
    return result.data;
  }
  throw new ApiError(
    result.response.status,
    readErrorMessage(result.error) ?? "Сервис временно недоступен",
  );
}

function unwrapAuthenticated<T>(result: ApiResult<T>): T {
  notifyAuthenticationRequired(result.response);
  return unwrap(result);
}

function unwrapVoid(result: ApiResult<never>): void {
  if (result.response.ok) {
    return;
  }
  throw new ApiError(
    result.response.status,
    readErrorMessage(result.error) ?? "Сервис временно недоступен",
  );
}

function unwrapVoidAuthenticated(result: ApiResult<never>): void {
  notifyAuthenticationRequired(result.response);
  unwrapVoid(result);
}

function notifyAuthenticationRequired(response: Response) {
  if (response.status === 401) {
    window.dispatchEvent(new Event(AUTHENTICATION_REQUIRED_EVENT));
  }
}

function readErrorMessage(value: unknown): string | undefined {
  if (typeof value !== "object" || value === null || !("error" in value)) return undefined;
  const error = value.error;
  if (typeof error !== "object" || error === null || !("message" in error)) return undefined;
  return typeof error.message === "string" ? error.message : undefined;
}
