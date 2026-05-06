FROM golang:1.24-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git



ENV GONOSUMDB=*

COPY go.work go.work.sum ./
COPY libs/ ./libs/
COPY cmd/predsx/ ./cmd/predsx/
COPY services/ ./services/

WORKDIR /app/services/market-discovery
RUN go build -p 1 -o /market-discovery .

FROM golang:1.24-alpine
COPY --from=builder /market-discovery /market-discovery
CMD ["/market-discovery"]
