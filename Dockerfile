# Multi-stage build. Pure-Go SQLite (modernc.org/sqlite) ⇒ static CGO_ENABLED=0
# build, so the runtime image carries no web assets (templates/static embedded).

FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o main ./cmd/main.go

FROM alpine:3.20 AS deploy
RUN apk add --no-cache tzdata ca-certificates wget
RUN adduser -S -u 1001 appuser
ENV TZ=Asia/Almaty
WORKDIR /app
COPY --from=build /app/main ./main
RUN mkdir -p /app/data && chown appuser /app/data
EXPOSE 8080
USER appuser
ENTRYPOINT ["./main"]
