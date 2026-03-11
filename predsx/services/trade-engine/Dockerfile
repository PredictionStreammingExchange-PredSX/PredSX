FROM golang:1.23-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git



ENV GONOSUMDB=*

COPY go.work go.work.sum ./
COPY libs/ ./libs/
COPY cmd/predsx/ ./cmd/predsx/
COPY services/ ./services/

WORKDIR /app/services/trade-engine
RUN go build -o /trade-engine .

FROM golang:1.23-alpine
COPY --from=builder /trade-engine /trade-engine
CMD ["/trade-engine"]
