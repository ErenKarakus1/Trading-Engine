FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /out/trading-engine ./cmd/trading-engine

FROM alpine:3.22

WORKDIR /app
COPY --from=build /out/trading-engine /app/trading-engine

EXPOSE 8080
ENTRYPOINT ["/app/trading-engine"]
