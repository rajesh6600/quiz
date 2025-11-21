# # Start from the official Go image for building the binary
# FROM golang:1.25.0-alpine AS builder

# # Set environment variables
# ENV CGO_ENABLED=0 \
#     GOOS=linux \
#     GOARCH=amd64

# # Set working directory
# WORKDIR /app

# # Copy Go module files
# COPY go.mod ./
# # If you have go.sum later, you can copy it too
# # COPY go.sum ./

# # Download dependencies (currently none, but keeps it ready)
# RUN go mod tidy

# # Copy the rest of the source code
# COPY . .

# # Build the binary
# RUN go build -o gemini-wrapper main.go

# # Use a minimal image for the final container
# FROM alpine:latest

# # Set timezone if needed
# RUN apk add --no-cache tzdata

# WORKDIR /app

# # Copy the binary from the builder stage
# COPY --from=builder /app/gemini-wrapper .

# # Expose the port the server will run on
# EXPOSE 9090

# # Set default environment variables (can be overridden)
# ENV PORT=9090 \
#     GEMINI_MODEL=models/gemini-1.5-flash

# # Run the binary
# CMD ["./gemini-wrapper"]
