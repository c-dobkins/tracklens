# syntax=docker/dockerfile:1
FROM node:26.12.1-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build # writes /src/internal/webui/dist

FROM golang:1.27.2 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -tags webui -ldflags "-s -w -X main.version=${VERSION}" -o /out/tracklens ./cmd/tracklens

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source="https://github.com/c-dobkins/tracklens" \
      org.opencontainers.image.description="Summarise Toggl Track time entries and flag data-quality issues" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/tracklens /tracklens
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/tracklens"]
