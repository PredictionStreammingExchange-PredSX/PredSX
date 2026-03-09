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
COPY libs/postgres-client libs/postgres-client
COPY libs/retry-utils libs/retry-utils
COPY libs/schemas libs/schemas
COPY libs/service libs/service

COPY services/backfill services/backfill
RUN cp /app/go.work.sum /app/services/backfill/go.sum

WORKDIR /app/services/backfill
RUN go build -o /backfill .

FROM golang:1.23-alpine
COPY --from=builder /backfill /backfill
CMD ["/backfill"]
