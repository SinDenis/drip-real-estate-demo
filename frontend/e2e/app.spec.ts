import { expect, test, type Browser } from "@playwright/test";

test("@bootstrap administrator activates the account and manages employee access", async ({
  browser,
  isMobile,
  page,
}) => {
  test.skip(isMobile, "bootstrap activation runs once against the shared E2E database");

  await page.goto("/sessions");

  await expect(page.getByRole("heading", { name: "Вход" })).toBeVisible();
  await page.getByLabel("Имя входа").fill("admin");
  await page.getByLabel("Пароль").fill("Bootstrap-Admin-2026!");
  await page.getByRole("button", { name: "Войти" }).click();

  await expect(page.getByRole("heading", { name: "Создайте постоянный пароль" })).toBeVisible();
  await page.getByLabel("Новый пароль", { exact: true }).fill("Reliable analytics access 2026");
  await page.getByLabel("Повторите новый пароль").fill("Reliable analytics access 2026");
  await page.getByRole("button", { name: "Сохранить пароль" }).click();

  await expect(page.getByRole("heading", { name: "Аналитические сессии" })).toBeVisible();
  await expect(page.getByText("Пока нет аналитических сессий")).toBeVisible();

  await page.getByRole("button", { name: "Открыть меню профиля" }).click();
  await page.getByRole("menuitem", { name: "Сотрудники" }).click();
  await expect(page.getByRole("heading", { name: "Сотрудники" })).toBeVisible();

  await page.getByRole("button", { name: "Добавить сотрудника" }).click();
  await page.getByLabel("Имя входа").fill("ivan.petrov");
  await page.getByLabel("Отображаемое имя").fill("Иван Петров");
  await page.getByRole("button", { name: "Создать аккаунт" }).click();

  await expect(page.getByRole("heading", { name: "Временный пароль" })).toBeVisible();
  await expect(page.getByText("Пароль будет показан только сейчас")).toBeVisible();
  const temporaryPassword = await page
    .getByRole("textbox", { name: "Временный пароль" })
    .inputValue();
  expect(temporaryPassword).toMatch(/.{20,}/);
  await page.getByRole("button", { name: "Готово" }).click();

  const { employeeContext, employeePage } = await activateEmployee(browser, temporaryPassword);
  await expect(employeePage.getByRole("heading", { name: "Аналитические сессии" })).toBeVisible();

  const forbidden = await employeePage.request.get("/api/v1/employees");
  expect(forbidden.status()).toBe(403);

  const employeeRow = page.getByRole("row", { name: /Иван Петров/ });
  await employeeRow.getByRole("button", { name: "Деактивировать" }).click();
  await expect(employeeRow.getByText("Деактивирована")).toBeVisible();
  await employeePage.reload();
  await expect(employeePage.getByRole("heading", { name: "Вход" })).toBeVisible();

  await employeeRow.getByRole("button", { name: "Восстановить" }).click();
  await expect(page.getByRole("heading", { name: "Временный пароль" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Временный пароль" })).not.toHaveValue(
    temporaryPassword,
  );
  await page.getByRole("button", { name: "Готово" }).click();
  await employeeContext.close();

  const administratorRow = page.getByRole("row", { name: /Аналитик-администратор.*admin/ });
  await administratorRow.getByRole("button", { name: "Деактивировать" }).click();
  await expect(page.getByText("Нельзя изменить последнего активного администратора")).toBeVisible();

  await page.reload();

  await expect(page.getByRole("heading", { name: "Сотрудники" })).toBeVisible();
});

async function activateEmployee(browser: Browser, temporaryPassword: string) {
  const employeeContext = await browser.newContext();
  const employeePage = await employeeContext.newPage();
  await employeePage.goto("/sessions");
  await employeePage.getByLabel("Имя входа").fill("ivan.petrov");
  await employeePage.getByLabel("Пароль").fill(temporaryPassword);
  await employeePage.getByRole("button", { name: "Войти" }).click();
  await employeePage
    .getByLabel("Новый пароль", { exact: true })
    .fill("Reliable employee access 2026");
  await employeePage.getByLabel("Повторите новый пароль").fill("Reliable employee access 2026");
  await employeePage.getByRole("button", { name: "Сохранить пароль" }).click();
  return { employeeContext, employeePage };
}

test("@workspace opens shared sessions from a remembered adaptive sidebar", async ({
  isMobile,
  page,
}) => {
  const selectedSessionId = "11111111-1111-4111-8111-111111111111";
  await page.goto(`/sessions/${selectedSessionId}`);
  await page.getByLabel("Имя входа").fill("admin");
  await page.getByLabel("Пароль").fill("Reliable analytics access 2026");
  await page.getByRole("button", { name: "Войти" }).click();

  await expect(page.getByRole("heading", { name: "Москва - ЖК Северный" })).toBeVisible();
  await expect(page.getByText("Канонические фильтры")).toBeVisible();
  await expect(page.getByText("Первичная недвижимость", { exact: true })).toBeVisible();

  if (isMobile) {
    await page.getByRole("button", { name: "Открыть список аналитических сессий" }).click();
    await expect(
      page.getByRole("navigation", { name: "Общие аналитические сессии" }),
    ).toBeVisible();
    await page
      .getByRole("navigation", { name: "Общие аналитические сессии" })
      .getByRole("button", { name: /2 регионов - 2 ЖК/ })
      .click();
    await expect(page.getByRole("heading", { name: "2 регионов - 2 ЖК" })).toBeVisible();
    await expect(
      page.getByRole("navigation", { name: "Общие аналитические сессии" }),
    ).not.toBeVisible();
    const hasNoHorizontalOverflow = await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    );
    expect(hasNoHorizontalOverflow).toBe(true);
  } else {
    const navigation = page.getByRole("navigation", { name: "Общие аналитические сессии" });
    await expect(navigation.getByText("Активные")).toBeVisible();
    await expect(navigation.getByText("Недавние")).toBeVisible();
    await navigation.getByRole("button", { name: "Показать еще" }).click();
    await expect(navigation.getByText("Калужская область - ЖК 31")).toBeVisible();
    await page.getByRole("button", { name: "Свернуть боковую панель" }).click();
    await expect(page.getByRole("button", { name: "Развернуть боковую панель" })).toBeVisible();
    await page.reload();
    await expect(page.getByRole("button", { name: "Развернуть боковую панель" })).toBeVisible();
  }

  await page.goto("/sessions/99999999-9999-4999-8999-999999999999");
  await expect(page.getByText("Сессия не найдена")).toBeVisible();
  await page.goto("/sessions/33333333-3333-4333-8333-333333333333");
  await expect(page.getByText("Сессия не найдена")).toBeVisible();
});

test("@catalog-happy selects a current market with keyboard on desktop and mobile", async ({
  page,
}) => {
  await loginAdministrator(page, "/sessions/new");
  await expect(page.getByRole("heading", { name: "Выберите текущий рынок" })).toBeVisible();

  await selectByKeyboard(page, "Регионы", "Москва");
  await selectByKeyboard(page, "Жилые комплексы", "ЖК Северный");
  await selectByKeyboard(page, "Сегменты недвижимости", "Жилые помещения");

  await expect(page.getByRole("button", { name: "Создать сессию" })).toBeEnabled();
  await expect(page.getByText("Период анализа")).toHaveCount(0);
  const hasNoHorizontalOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth <= window.innerWidth,
  );
  expect(hasNoHorizontalOverflow).toBe(true);
});

test("@session-create creates and reloads a warm-cache dashboard", async ({ page }) => {
  await loginAdministrator(page, "/sessions/new");
  await selectByKeyboard(page, "Регионы", "Москва");
  await selectByKeyboard(page, "Жилые комплексы", "ЖК Северный");
  await selectByKeyboard(page, "Сегменты недвижимости", "Жилые помещения");
  await page.getByRole("button", { name: "Создать сессию" }).click();

  await expect(page).toHaveURL(/\/sessions\/[0-9a-f-]{36}$/);
  const firstURL = page.url();
  await expect(page.getByRole("heading", { name: /Москва - ЖК Северный/ })).toBeVisible();
  await expect(page.getByText("Фактическое окно наблюдения")).toBeVisible();
  await expect(
    page.getByRole("rowgroup").getByText("Жилые помещения", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("Готово", { exact: true })).toBeVisible();
  await expect(page.getByText("Накопительное количество ДДУ")).toBeVisible();
  await expect(page.getByText("Нет данных", { exact: true })).toBeVisible();
  await expect(page.getByText("0", { exact: true })).toHaveCount(0);

  await page.reload();
  await expect(page).toHaveURL(firstURL);
  await expect(page.getByRole("heading", { name: /Москва - ЖК Северный/ })).toBeVisible();

  await page.goto("/sessions/new");
  await selectByKeyboard(page, "Регионы", "Москва");
  await selectByKeyboard(page, "Жилые комплексы", "ЖК Северный");
  await selectByKeyboard(page, "Сегменты недвижимости", "Жилые помещения");
  await page.getByRole("button", { name: "Создать сессию" }).click();
  await expect(page).toHaveURL(/\/sessions\/[0-9a-f-]{36}$/);
  expect(page.url()).not.toBe(firstURL);
});

test("@session-collect creates a cold session and opens its current ЕИСЖС dashboard", async ({
  page,
}) => {
  let releaseFirstCollect: () => void = () => undefined;
  const firstCollectReleased = new Promise<void>((resolve) => {
    releaseFirstCollect = resolve;
  });
  let observeFirstCollect: () => void = () => undefined;
  const firstCollectObserved = new Promise<void>((resolve) => {
    observeFirstCollect = resolve;
  });
  let collectRequests = 0;
  let firstCollectIsReleased = false;
  let secondCollectArrivedEarly = false;
  await page.route("**/api/v1/sessions/*/collect", async (route) => {
    collectRequests += 1;
    if (collectRequests > 1 && !firstCollectIsReleased) {
      secondCollectArrivedEarly = true;
    }
    if (collectRequests === 1) {
      observeFirstCollect();
      await firstCollectReleased;
    }
    await route.continue();
  });
  await loginAdministrator(page, "/sessions/new");
  await selectByKeyboard(page, "Регионы", "Москва");
  await selectByKeyboard(page, "Жилые комплексы", "ЖК Центральный");
  await selectByKeyboard(page, "Сегменты недвижимости", "Жилые помещения");
  await page.getByRole("button", { name: "Создать сессию" }).click();

  await expect(page).toHaveURL(/\/sessions\/[0-9a-f-]{36}$/);
  await firstCollectObserved;
  await expect(page.getByText("0 запросов ЕИСЖС - выполняется планирование")).toBeVisible();
  await expect(page.getByRole("progressbar")).toHaveCount(0);
  await expect(page.getByText("Текущий срез ЕИСЖС")).toHaveCount(0);
  await page.waitForTimeout(300);
  expect(collectRequests).toBe(1);
  expect(secondCollectArrivedEarly).toBe(false);
  firstCollectIsReleased = true;
  releaseFirstCollect();
  await expect(page.getByText("Текущий срез ЕИСЖС")).toBeVisible({ timeout: 25_000 });
  await expect(page.getByText("Накопительное количество ДДУ")).toBeVisible();
  await expect(page.getByText("17", { exact: true })).toBeVisible();
  await expect(page.getByText("2 из 2 запросов ЕИСЖС")).toBeVisible();
  expect(collectRequests).toBe(2);
  await page.reload();
  await expect(page.getByText("Текущий срез ЕИСЖС")).toBeVisible();
  const hasNoHorizontalOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth <= window.innerWidth,
  );
  expect(hasNoHorizontalOverflow).toBe(true);
});

test("@session-resume continues paged unique-object collection after reload", async ({
  isMobile,
  page,
}) => {
  test.setTimeout(75_000);
  test.skip(isMobile, "the detailed progress remains covered by the mobile cold-session journey");
  const mode = await page.request.post(
    "http://fake-eiszhs:8090/admin/current-mode?value=paged-duplicates",
  );
  expect(mode.ok()).toBe(true);
  const before = (await (await page.request.get("http://fake-eiszhs:8090/admin/stats")).json()) as {
    currentRequests: number;
  };
  let collectRequests = 0;
  let paused = false;
  await page.route("**/api/v1/sessions/*/collect", async (route) => {
    collectRequests += 1;
    if (collectRequests === 3 && !paused) {
      paused = true;
      await route.abort("failed");
      return;
    }
    await route.continue();
  });

  await loginAdministrator(page, "/sessions/new");
  await selectByKeyboard(page, "Регионы", "Москва");
  await selectByKeyboard(page, "Жилые комплексы", "ЖК Северный");
  await selectByKeyboard(page, "Жилые комплексы", "ЖК Центральный");
  await selectByKeyboard(page, "Сегменты недвижимости", "Жилые помещения");
  await page.getByRole("button", { name: "Создать сессию" }).click();

  await expect(page.getByRole("alert")).toContainText(
    "Не удалось продолжить получение данных ЕИСЖС",
    { timeout: 25_000 },
  );
  await expect(page.getByText("2 запросов ЕИСЖС - выполняется планирование")).toBeVisible();
  await expect(page.getByText("Найдено объектов: 2")).toBeVisible();
  await expect(page.getByText("Текущий ЖК ЕИСЖС: 1002")).toBeVisible();
  expect(collectRequests).toBe(3);

  await page.reload();
  await expect(page.getByText("Текущий срез ЕИСЖС")).toBeVisible({ timeout: 45_000 });
  await expect(page.getByText("6 из 6 запросов ЕИСЖС")).toBeVisible({ timeout: 45_000 });
  await expect(page.getByText("Найдено объектов: 3")).toBeVisible();
  await expect(page.getByText("Получено: 3")).toBeVisible();
  await expect(page.getByText("Осталось запросов: 0")).toBeVisible();
  await expect(page.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "100");
  const after = (await (await page.request.get("http://fake-eiszhs:8090/admin/stats")).json()) as {
    currentRequests: number;
  };
  expect(after.currentRequests - before.currentRequests).toBe(6);
  expect(collectRequests).toBe(7);
  const reset = await page.request.post("http://fake-eiszhs:8090/admin/current-mode?value=healthy");
  expect(reset.ok()).toBe(true);
});

test("@catalog-stale shows observed and expiry dates with cached complexes", async ({
  isMobile,
  page,
}) => {
  test.skip(isMobile, "mobile current-market flow is covered by @catalog-happy");
  await loginAdministrator(page, "/sessions/new");
  await selectByKeyboard(page, "Регионы", "Москва");

  await expect(page.getByRole("status")).toContainText("устаревший справочник ЖК");
  await page.getByLabel("Жилые комплексы").click();
  await expect(page.getByText("ЖК Северный, регион 77", { exact: true })).toBeVisible();
});

test("@catalog-retry recovers from no-cache ЕИСЖС failure", async ({ isMobile, page }) => {
  test.skip(isMobile, "mobile current-market flow is covered by @catalog-happy");
  await loginAdministrator(page, "/sessions/new");
  await selectByKeyboard(page, "Регионы", "Москва");
  await expect(page.getByRole("alert")).toContainText("Справочник ЖК временно недоступен");

  const recovered = await page.request.post("http://fake-eiszhs:8090/admin/mode?value=healthy");
  expect(recovered.ok()).toBe(true);
  await page.getByRole("button", { name: "Повторить" }).click();
  await page.getByLabel("Жилые комплексы").click();
  await expect(page.getByText("ЖК Северный, регион 77", { exact: true })).toBeVisible({
    timeout: 15_000,
  });
});

async function loginAdministrator(page: import("@playwright/test").Page, path: string) {
  await page.goto(path);
  await page.getByLabel("Имя входа").fill("admin");
  await page.getByLabel("Пароль").fill("Reliable analytics access 2026");
  await page.getByRole("button", { name: "Войти" }).click();
}

async function selectByKeyboard(
  page: import("@playwright/test").Page,
  label: string,
  search: string,
) {
  const select = page.getByLabel(label);
  await expect(select).toBeEnabled();
  await select.focus();
  await select.fill(search);
  await page.keyboard.press("Enter");
  await page.keyboard.press("Escape");
}
