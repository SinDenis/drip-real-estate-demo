# Карта репозитория по задачам

Карта помогает выбрать следующий источник. Она не подменяет чтение кода и не загружается автоматически только из-за своего имени.

| Задача | Где начать | Что проверить дальше |
|---|---|---|
| Изменить HTTP-ответ | [OpenAPI](api/openapi.yaml), [HTTP handler](backend/internal/adapters/httpapi/handler.go) | Generated Go и frontend schema; [HTTP-тесты](backend/internal/adapters/httpapi/handler_test.go) |
| Понять аналитический показатель | [Доменный словарь](CONTEXT.md) | [Типы current collection](backend/internal/application/current_collection.go), [отображение сессии](frontend/src/SessionOverview.tsx) |
| Разобраться в создании сессии | [Application](backend/internal/application/analytical_sessions.go) | [HTTP translation](backend/internal/adapters/httpapi/resources.go), [immutability и idempotency в миграции](backend/internal/adapters/postgres/migrations/003_analytical_session_creation.sql) |
| Сделать статистику БД | [Makefile](Makefile), [PostgreSQL store](backend/internal/adapters/postgres/store.go) | [Решение о сессиях](docs/adr/0001-immutable-sessions.md), [интерфейс диагностики](docs/adr/0002-diagnostics-interface.md); enum статусов, пустая база, read-only проверка |
| Проверить локальные команды | [Тесты команд](tests/local_commands_test.sh) | Подмена DOCKER, пробелы в путях и отсутствие интерпретации SQL как shell |
| Проверить UI | [App tests](frontend/src/App.test.tsx), [browser E2E](frontend/e2e/app.spec.ts) | Доступный DOM, network seam, loading/error/empty/success |
| Выпустить изменение | [Development workflow](docs/development.md), [Validate](.github/workflows/validate.yml) | Полные проверки, generated consistency, runtime Dockerfile; deployment не настроен |

Направление основного запроса: HTTP adapter → application → port → PostgreSQL или внешний adapter. Локальная диагностическая команда использует Compose и PostgreSQL напрямую через существующий интерфейс Makefile.

Источник карты: базовый код `d037c2c`. Перед изменением проверь актуальность названных файлов и команд.
