FROM golang:1.24-alpine AS builder
ENV GOMEMLIMIT=1GiB
ENV GOGC=20

WORKDIR /app

RUN apk add --no-cache git



ENV GONOSUMDB=*

COPY go.work go.work.sum ./
COPY libs/ ./libs/
COPY cmd/predsx/ ./cmd/predsx/
COPY services/ ./services/

WORKDIR /app/services/orderbook-engine
RUN go build -p 1 -o /orderbook-engine .

FROM golang:1.24-alpine
ENV GOMEMLIMIT=1GiB
ENV GOGC=20
COPY --from=builder /orderbook-engine /orderbook-engine
CMD ["/orderbook-engine"]
