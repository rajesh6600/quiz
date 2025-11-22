# syntax=docker/dockerfile:1

FROM golang:1.25.0-alpine AS builder
WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -o /out/quiz-platform ./cmd/api

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /app

ENV APP_ENV=production
COPY --from=builder /out/quiz-platform /usr/local/bin/app

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/app"]

