# Multi-stage build
# Stage 1: Build the Go application
FROM golang:1.24-alpine AS builder

# Install build dependencies including CGO requirements
RUN apk add --no-cache git ca-certificates tzdata gcc musl-dev pkgconfig opus-dev

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with CGO enabled
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o xiaozhi-server ./src/main.go

# Stage 2: Create the final image
FROM alpine:latest

# Install runtime dependencies including opus
RUN apk add --no-cache \
    ca-certificates \
    sqlite-libs \
    procps \
    opus

# Create app directory
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/xiaozhi-server .

# Copy configuration file
COPY --from=builder /app/config.yaml .

# Copy music directory if it exists
COPY --from=builder /app/music ./music

# Make the binary executable
RUN chmod +x ./xiaozhi-server

# Expose ports
EXPOSE 8000 8080

# Run the application
CMD ["./xiaozhi-server"]
