# Encore builds are done via `encore build docker wabantu`
# which produces an optimized image. This file is for reference / custom builds.
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o api-server ./...

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/api-server /usr/local/bin/
EXPOSE 4000
CMD ["api-server"]
