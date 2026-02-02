# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS build
WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/metrics-service ./cmd

FROM alpine:3.19
WORKDIR /app

RUN apk add --no-cache ca-certificates
COPY --from=build /out/metrics-service /app/metrics-service

EXPOSE 8080
ENTRYPOINT ["/app/metrics-service"]