# syntax=docker/dockerfile:1
FROM golang:1.27.2 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/tracklens ./cmd/tracklens

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source="https://github.com/c-dobkins/p1" \
      org.opencontainers.image.description="Summarise Toggl Track time entries and flag data-quality issues" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/tracklens /tracklens
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/tracklens"]
