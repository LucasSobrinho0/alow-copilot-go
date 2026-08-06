FROM golang:1.25-alpine AS build

WORKDIR /app

COPY . .
RUN go vet ./...
RUN go test ./...
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/alow-invoice-extractor .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates poppler-utils \
    && addgroup -S app \
    && adduser -S -G app app

COPY --from=build /bin/alow-invoice-extractor /usr/local/bin/alow-invoice-extractor

USER app
EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=6 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["alow-invoice-extractor"]
