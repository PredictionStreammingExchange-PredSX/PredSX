FROM golang:1.24-alpine AS builder
ENV GOMEMLIMIT=1GiB
ENV GOGC=20

WORKDIR /app
COPY go.work go.work.sum ./
COPY libs/ libs/
COPY cmd/ cmd/
COPY services/ services/

WORKDIR /app/services/indexer
RUN go build -p 1 -o indexer main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/services/indexer/indexer .

CMD ["./indexer"]
