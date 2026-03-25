FROM golang:1.23-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

ENV GONOSUMDB=*

COPY go.work go.work.sum ./
COPY libs/ ./libs/
COPY cmd/predsx/ ./cmd/predsx/
COPY services/ ./services/

WORKDIR /app/services/analyzer
RUN go build -o /analyzer .

FROM golang:1.23-alpine
COPY --from=builder /analyzer /analyzer
CMD ["/analyzer"]
