# Build stage
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /jhub-ssh ./cmd/jhub-ssh

# Runtime stage — distroless for minimal attack surface
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /jhub-ssh /jhub-ssh
EXPOSE 8022
ENTRYPOINT ["/jhub-ssh", "serve"]
