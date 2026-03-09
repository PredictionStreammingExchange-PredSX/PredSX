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
COPY libs/retry-utils libs/retry-utils
COPY libs/schemas libs/schemas
COPY libs/service libs/service
COPY libs/websocket-client libs/websocket-client

COPY services/stream services/stream
RUN cp /app/go.work.sum /app/services/stream/go.sum

WORKDIR /app/services/stream
RUN go build -o /stream .

FROM golang:1.23-alpine
COPY --from=builder /stream /stream
CMD ["/stream"]
