FROM node:24.20.0-bookworm-slim AS node
FROM golang:1.27.0-bookworm
COPY --from=node /usr/local/bin/node /usr/local/bin/node
WORKDIR /workspace
