FROM golang:1.23-alpine AS builder
RUN apk add --no-cache git
ENV GONOSUMDB=*
ENV GOWORK=off
ENV GOFLAGS=-mod=mod

WORKDIR /app

COPY go.work.sum ./

COPY libs/config libs/config
COPY libs/kafka-client libs/kafka-client
COPY libs/logger libs/logger
COPY libs/redis-client libs/redis-client
COPY libs/retry-utils libs/retry-utils
COPY libs/schemas libs/schemas
COPY libs/service libs/service

COPY services/trade-engine services/trade-engine
RUN cp /app/go.work.sum /app/services/trade-engine/go.sum

WORKDIR /app/services/trade-engine
RUN go build -o /trade-engine .

FROM golang:1.23-alpine
COPY --from=builder /trade-engine /trade-engine
CMD ["/trade-engine"]
