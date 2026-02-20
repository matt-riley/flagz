# syntax=docker/dockerfile:1

FROM golang:1.26.0 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY api ./api
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/server /app/server

EXPOSE 8080 9090

USER nonroot:nonroot
ENTRYPOINT ["/app/server"]
