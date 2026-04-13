# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

ENV GOTOOLCHAIN=auto

# Copy source code first (needed for go mod tidy to see packages)
COPY . .

# Synchronize dependencies and regenerate go.sum
RUN go mod tidy

# Download dependencies
RUN go mod download

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
ENV PORT=7860
EXPOSE 7860

# Run the binary
CMD ["/app/met-quest-server"]
