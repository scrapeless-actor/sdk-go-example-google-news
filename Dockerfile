# build stage
FROM golang:1.24.1 AS build-env
ENV GOPROXY=https://goproxy.cn,direct

RUN mkdir -p /workspace
ADD ./ /workspace

WORKDIR /workspace

RUN go mod download
RUN go mod tidy
RUN go build -ldflags "-s -w" -o goapp

ENTRYPOINT ["./goapp"]
