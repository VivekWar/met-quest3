# Build stage
FROM golang:1.23-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git

# Copy root-level data and backend code
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .

# Build a statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o met-quest-server main.go

# Final stage
FROM alpine:latest
WORKDIR /app
RUN apk --no-cache add ca-certificates

# Copy from builder
COPY --from=builder /app/met-quest-server .
# Copy data from the ROOT of the repository
COPY data ./data

# Production environment variables
ENV PORT=7860
ENV GIN_MODE=release
EXPOSE 7860

# Force execution permissions
RUN chmod +x /app/met-quest-server

# Instant Startup
CMD ["./met-quest-server"]
