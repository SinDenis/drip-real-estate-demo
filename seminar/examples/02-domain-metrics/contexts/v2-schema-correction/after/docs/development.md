# Разработка, БД, проверка и релиз

## Архитектура и соглашения

- `backend/internal/application` содержит правила и порты. Он не импортирует HTTP, PostgreSQL или S3-адаптеры.
- `backend/internal/adapters/httpapi` переводит HTTP в application. `generated/api.gen.go` создаётся из `api/openapi.yaml`.
- `frontend/src/api` - граница production HTTP. `schema.d.ts` создаётся из того же OpenAPI.
- Пиши Go с `gofmt`, TypeScript и CSS с установленным Prettier. Не создавай второй механизм для операции, у которой уже есть интерфейс проекта.
- Пример изменения контракта: изменить YAML, выполнить `make generate`, проверить HTTP-ответ и актуальность обоих generated-файлов.

## Команды

| Цель | Команда |
|---|---|
| Доступные команды | `make help` |
| Генерация Go и TypeScript из OpenAPI | `make generate` |
| Backend-тесты | `make test-backend` |
| Один HTTP-тест | `cd backend && go test ./internal/adapters/httpapi -run TestName -count=1` |
| Frontend-тесты | `make test-frontend` |
| Проверка границ | `make architecture` |
| Проверки без Docker | `make verify-fast` |
| Контейнерные HTTP и browser E2E | `make e2e` |
| Полная проверка | `make verify` |
| Локальный SQL | `make db-query QUERY='SELECT count(*) FROM analytical_session'` |

Текущий Makefile уже содержит эти команды. В сравнении вариантов контекста сам Makefile и доступные инструменты одинаковы.

## БД

Локальный PostgreSQL запускается через Compose; его сервис называется `postgres`, база и локальная роль - `analytics`. Миграции находятся в `backend/internal/adapters/postgres/migrations`. Диагностика использует SELECT и интерфейс `make db-query`. Не удаляй тома и не используй реальные данные для учебных проверок.

Новые диагностические команды должны работать с пустой БД и не менять записи. Их проверяют на отдельной синтетической базе. Наличие записи в БД и наличие доступного показателя - разные вещи.

## Проверка и релиз

Итерация начинается с узкой проверки изменённой публичной границы. Перед обычным handoff нужны `make verify-fast`, `make e2e` и зелёный Validate в GitHub Actions. Runtime-образ строится из Dockerfile; frontend включён в него. В репозитории нет команды `make deploy` и нет настроенного production deployment.

В автоматическом учебном прогоне агент использует узкие native-команды. Для изменения liveness достаточно `make generate PNPM=pnpm` и `cd backend && go test ./internal/adapters/httpapi -count=1`. Полный verification и Docker запускает evaluator отдельно. Не считай текст «готово» или успешное завершение процесса доказательством правильного поведения.
