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

WORKDIR /app/services/stream
RUN go build -p 1 -o /stream .

FROM golang:1.24-alpine
ENV GOMEMLIMIT=1GiB
ENV GOGC=20
COPY --from=builder /stream /stream
CMD ["/stream"]
