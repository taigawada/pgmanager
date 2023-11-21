FROM golang:1.21.4-alpine AS builder

ENV GOPATH=
WORKDIR /

COPY ./go.mod .
COPY ./go.sum .

RUN go mod download

COPY ./main.go .

RUN GOOS=linux GOARCH=arm64 go build -o pgmanager main.go

FROM arm64v8/alpine:latest
WORKDIR /bin/pgmanager

COPY --from=builder /pgmanager .

RUN export export PATH="/bin/pgmanager:$PATH"