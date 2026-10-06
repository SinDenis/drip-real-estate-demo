ARG GO_VERSION=1.27.0
ARG NODE_VERSION=24.20.0
ARG PNPM_VERSION=11.25.0

FROM node:${NODE_VERSION}-bookworm-slim AS frontend-dependencies
ARG PNPM_VERSION
ENV PNPM_HOME=/pnpm
ENV PATH=${PNPM_HOME}:${PATH}
RUN corepack enable && corepack prepare pnpm@${PNPM_VERSION} --activate
WORKDIR /workspace/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

FROM frontend-dependencies AS frontend-build
COPY api /workspace/api
COPY frontend ./
RUN pnpm generate:api && pnpm build

FROM golang:${GO_VERSION}-bookworm AS backend-build
WORKDIR /workspace/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/eiszhsfake ./cmd/eiszhsfake

FROM debian:bookworm-slim AS fake-eiszhs
RUN apt-get update \
    && apt-get install --yes --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*
COPY --from=backend-build /out/eiszhsfake /eiszhsfake
EXPOSE 8090
ENTRYPOINT ["/eiszhsfake"]

FROM backend-build AS backend-e2e
ENV E2E_BASE_URL=http://app:8080
CMD ["go", "test", "-tags=e2e", "-count=1", "./e2e"]

FROM mcr.microsoft.com/playwright:v1.63.0-noble AS frontend-e2e
ARG PNPM_VERSION
COPY --from=frontend-dependencies /usr/local/bin/node /usr/local/bin/node
RUN npm install --global pnpm@${PNPM_VERSION}
WORKDIR /workspace/frontend
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY api /workspace/api
COPY frontend ./
CMD ["pnpm", "e2e"]

FROM debian:bookworm-slim AS runtime
RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system app \
    && useradd --system --gid app --home-dir /app app
WORKDIR /app
COPY --from=backend-build /out/api /app/api
COPY --from=frontend-build /workspace/frontend/dist /app/public
ENV APP_PORT=8080
ENV STATIC_DIR=/app/public
EXPOSE 8080
USER app
HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=12 \
  CMD ["curl", "--fail", "--silent", "--show-error", "http://127.0.0.1:8080/health/live"]
ENTRYPOINT ["/app/api"]
