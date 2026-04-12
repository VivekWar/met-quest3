# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application statically
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/met-quest-server main.go

# Final stage
FROM alpine:latest

WORKDIR /app

# Add CA certificates for HTTPS calls to Gemini API
RUN apk --no-cache add ca-certificates

# Copy the binary and data from the builder stage
COPY --from=builder /app/met-quest-server .
COPY data ./data

# Expose port (Cloud Run sets PORT env var)
ENV PORT=8080
EXPOSE 8080

# Run the binary
CMD ["/app/met-quest-server"]
