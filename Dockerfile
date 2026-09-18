FROM golang:1.23-bookworm AS build

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dbops-server ./cmd/dbops-server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=build /out/dbops-server /usr/local/bin/dbops-server

VOLUME ["/data/dbops"]
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/dbops-server"]
