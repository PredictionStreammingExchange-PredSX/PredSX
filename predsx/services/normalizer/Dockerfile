FROM golang:1.23-alpine AS builder
RUN apk add --no-cache git
ENV GONOSUMDB=*
ENV GOWORK=off
ENV GOFLAGS=-mod=mod

WORKDIR /app

COPY go.work.sum ./

COPY libs/clickhouse-client libs/clickhouse-client
COPY libs/config libs/config
COPY libs/kafka-client libs/kafka-client
COPY libs/logger libs/logger
COPY libs/redis-client libs/redis-client
COPY libs/retry-utils libs/retry-utils
COPY libs/schemas libs/schemas
COPY libs/service libs/service

COPY services/normalizer services/normalizer
RUN cp /app/go.work.sum /app/services/normalizer/go.sum

WORKDIR /app/services/normalizer
RUN go build -o /normalizer .

FROM golang:1.23-alpine
COPY --from=builder /normalizer /normalizer
CMD ["/normalizer"]
