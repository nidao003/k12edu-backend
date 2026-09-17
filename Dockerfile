FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/k12edu-api ./cmd/api

FROM alpine:3.21
WORKDIR /app
COPY --from=build /out/k12edu-api /app/k12edu-api
COPY admin /app/admin
EXPOSE 8080
ENTRYPOINT ["/app/k12edu-api"]
