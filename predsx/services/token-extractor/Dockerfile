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

COPY services/token-extractor services/token-extractor
RUN cp /app/go.work.sum /app/services/token-extractor/go.sum

WORKDIR /app/services/token-extractor
RUN go build -o /token-extractor .

FROM golang:1.23-alpine
COPY --from=builder /token-extractor /token-extractor
CMD ["/token-extractor"]
