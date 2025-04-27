FROM golang:alpine AS builder

WORKDIR /build

COPY . .
ENTRYPOINT [ "sh", "-c", "cd /build && go run main.go" ]