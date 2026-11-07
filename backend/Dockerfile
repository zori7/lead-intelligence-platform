# Go API: static binary on distroless (no shell, runs as non-root).
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
EXPOSE 8080
# Migrations are embedded in the binary and applied on start (MIGRATE_ON_START).
HEALTHCHECK --interval=5s --timeout=3s --retries=10 CMD ["/api", "healthcheck"]
ENTRYPOINT ["/api"]
