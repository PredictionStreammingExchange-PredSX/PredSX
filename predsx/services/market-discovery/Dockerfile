FROM golang:1.23-alpine AS builder
RUN apk add --no-cache git
ENV GONOSUMDB=*
ENV GOWORK=off
ENV GOFLAGS=-mod=mod

WORKDIR /app

COPY go.work.sum ./

# Copy libs this service depends on
COPY libs/config libs/config
COPY libs/kafka-client libs/kafka-client
COPY libs/logger libs/logger
COPY libs/postgres-client libs/postgres-client
COPY libs/retry-utils libs/retry-utils
COPY libs/schemas libs/schemas
COPY libs/service libs/service

# Copy the service
COPY services/market-discovery services/market-discovery

# Copy go.work.sum as go.sum for this service so checksums don't need re-fetching
RUN cp /app/go.work.sum /app/services/market-discovery/go.sum

# Build from the service directory (so relative replace paths in go.mod work)
WORKDIR /app/services/market-discovery
RUN go build -o /market-discovery .

FROM golang:1.23-alpine
COPY --from=builder /market-discovery /market-discovery
CMD ["/market-discovery"]
