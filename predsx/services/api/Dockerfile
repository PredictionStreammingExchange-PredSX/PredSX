FROM golang:1.23-alpine AS builder
RUN apk add --no-cache git
ENV GONOSUMDB=*
ENV GOWORK=off
ENV GOFLAGS=-mod=mod

WORKDIR /app

COPY go.work.sum ./

COPY libs/clickhouse-client libs/clickhouse-client
COPY libs/config libs/config
COPY libs/logger libs/logger
COPY libs/redis-client libs/redis-client
COPY libs/retry-utils libs/retry-utils
COPY libs/service libs/service

COPY services/api services/api
RUN cp /app/go.work.sum /app/services/api/go.sum

WORKDIR /app/services/api
RUN go build -o /api .

FROM golang:1.23-alpine
COPY --from=builder /api /api
EXPOSE 8080
CMD ["/api"]
