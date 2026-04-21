FROM golang:1.25.3-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/http-mock ./cmd/http-mock

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /out/http-mock /usr/local/bin/http-mock

WORKDIR /data

EXPOSE 18080

ENTRYPOINT ["http-mock"]
CMD ["serve", "--routes", "routes.yaml", "--data-root", "/data", "--listen", ":18080"]
