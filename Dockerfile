# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app


RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download


COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o youtube-searchapi .


FROM alpine:latest

WORKDIR /app


RUN apk --no-cache add ca-certificates

COPY --from=builder /app/youtube-searchapi .
COPY --from=builder /app/config.yaml .

EXPOSE 8080

ENTRYPOINT ["./youtube-searchapi"]
CMD ["-config", "config.yaml"]
