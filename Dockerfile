# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy the backend go.mod and go.sum from the backend directory
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# Copy the entire backend directory into the build context
COPY backend/ .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/met-quest-server main.go

# Final stage
FROM alpine:latest

WORKDIR /app

# Add CA certificates for HTTPS calls to Gemini API
RUN apk --no-cache add ca-certificates

# Copy the binary from the builder stage
COPY --from=builder /app/met-quest-server .

# Copy the data directory from the project root
COPY data ./data

# Hugging Face Spaces use port 7860 by default
ENV PORT=7860
EXPOSE 7860

# Run the binary
CMD ["/app/met-quest-server"]
