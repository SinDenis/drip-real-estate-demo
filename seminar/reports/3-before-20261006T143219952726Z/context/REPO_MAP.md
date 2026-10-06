# Карта DRIP

HTTP handler вызывает application service. Service проверяет доступ и правила, затем вызывает repository port. PostgreSQL adapter реализует порт. Общий контракт генерируется для Go и TypeScript.

| Задача | Начать | Дальше |
|---|---|---|
| Добавить HTTP-операцию | [OpenAPI](api/openapi.yaml) | [NewHandler и health](backend/internal/adapters/httpapi/handler.go), [операции с сессиями](backend/internal/adapters/httpapi/resources.go) |
| Прочитать видимую сессию | [AccessService.GetAnalyticalSession](backend/internal/application/access.go) | [Store.GetAnalyticalSession](backend/internal/adapters/postgres/store.go), [схема](backend/internal/adapters/postgres/migrations/001_access.sql) |
| Понять показатель | [Доменный словарь](CONTEXT.md) | [CurrentDashboard](backend/internal/application/access.go), [сбор текущего среза](backend/internal/application/current_collection.go) |
| Создать сессию | [AnalyticalSessionService.Create](backend/internal/application/analytical_sessions.go) | [тесты создания](backend/internal/application/analytical_sessions_test.go) |
| Проверить результат | [HTTP-тесты](backend/internal/adapters/httpapi/handler_test.go) | [Makefile](Makefile), [границы модулей](backend/architecture) |

Техническая семантика: CumulativeDDUCount - nullable *int64; nil и указатель на 0 различаются. Отсутствующий CurrentDashboard означает отсутствие сохраненного результата, а не заполненный нулями dashboard. GetAnalyticalSession уже применяет авторизацию и скрытие удаленных сессий.
