FROM node:22-bookworm-slim AS web-build

WORKDIR /src
COPY web ./web
RUN cd web && npm ci --no-audit --no-fund && npm run build

FROM golang:1.23-bookworm AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
COPY --from=web-build /src/internal/httpapi/web/dist ./internal/httpapi/web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dbops-server ./cmd/dbops-server
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dbops-agent ./cmd/dbops-agent

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dbops-restore ./cmd/dbops-restore
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=build /out/dbops-server /usr/local/bin/dbops-server
COPY --from=build /out/dbops-agent /usr/local/bin/dbops-agent

COPY --from=build /out/dbops-restore /usr/local/bin/dbops-restore
COPY --from=build --chown=65532:65532 /out/data /data/dbops

VOLUME ["/data/dbops"]
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/dbops-server"]
