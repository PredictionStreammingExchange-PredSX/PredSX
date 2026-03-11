FROM golang:1.23-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git



ENV GONOSUMDB=*

COPY go.work go.work.sum ./
COPY libs/ ./libs/
COPY cmd/predsx/ ./cmd/predsx/
COPY services/ ./services/

WORKDIR /app/services/backfill
RUN go build -o /backfill .

FROM golang:1.23-alpine
COPY --from=builder /backfill /backfill
CMD ["/backfill"]
