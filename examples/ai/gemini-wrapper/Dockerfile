# Build stage
FROM golang:1.25.0-alpine AS builder

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

WORKDIR /app

# Copy module file (no go.sum because wrapper has no deps)
COPY go.mod ./

# Download modules (this will do nothing, but safe)
RUN go mod tidy

# Copy source
COPY . .

# Build binary
RUN go build -o gemini-wrapper main.go

# Final image
FROM alpine:latest
RUN apk add --no-cache tzdata

WORKDIR /app

COPY --from=builder /app/gemini-wrapper .

EXPOSE 9090

ENV PORT=9090 \
    GEMINI_MODEL=models/gemini-2.5-flash

CMD ["./gemini-wrapper"]

