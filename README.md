# Trip service

HTTP-сервис лабораторной работы №1: создаёт, возвращает и завершает поездки,
хранит данные в PostgreSQL и поддерживает идемпотентное создание поездки.

## Локальный запуск

Нужны Go 1.26, GNU Make, `tripgoctl` и запущенный кластер курса.

```bash
tripgoctl cluster start
tripgoctl environment start
tripgoctl connect
make migrate
make run
```

Проверка готовности: `curl -i localhost:8080/ready`.

Основные команды: `make migrate`, `make migrate-down`, `make generate`,
`make build`, `make run`.

## Переменные окружения

| Переменная | Назначение |
|---|---|
| `HTTP_ADDR` | адрес HTTP-сервера |
| `LOG_LEVEL` | уровень логирования |
| `SHUTDOWN_TIMEOUT` | общий таймаут остановки |
| `IDEMPOTENCY_TTL` | срок хранения ключа идемпотентности |
| `DATABASE_URL` | строка подключения PostgreSQL |
| `DATABASE_MAX_CONNS` / `DATABASE_MIN_CONNS` | границы пула |
| `DATABASE_MAX_CONN_LIFETIME` | срок жизни соединения |
| `DATABASE_CONNECT_TIMEOUT` | таймаут подключения |
| `DATABASE_QUERY_TIMEOUT` | таймаут запроса к БД |

## Решения

- Транзакции используют `Read Committed`: уникальные ограничения и блокировка
  строки обеспечивают нужную конкурентную корректность без более строгой
  изоляции. Менеджер кладёт `pgx.Tx` в `context`, откатывает её при ошибке или
  панике и переиспользует при вложенном `Do`.
- Две активные поездки одного водителя запрещены частичным уникальным индексом
  `trips_one_active_per_driver_idx`. Завершение сериализуется через
  `SELECT ... FOR UPDATE`.
- `Idempotency-Key` хранится 24 часа по умолчанию. Middleware берёт
  транзакционную advisory lock, сравнивает SHA-256 тела и сохраняет HTTP-ответ в
  той же транзакции, что поездку и историю. Повтор возвращает ту же поездку с
  `200`, другое тело — `409 idempotency_conflict`.

## Docker

```bash
docker compose -f deploy/compose.yaml up --build
curl -i localhost:8080/ready
docker compose -f deploy/compose.yaml down
```

Runtime-образ основан на distroless, содержит только бинарник и запускается от
пользователя `65532`. Размер собранного образа по `docker image ls` — 21,1 МБ.
