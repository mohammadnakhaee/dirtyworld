# ---- build stage ----
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

# ---- run stage ----
FROM alpine:3.20
WORKDIR /app
COPY --from=build /bin/server /app/server
COPY web ./web
EXPOSE 8080
CMD ["/app/server"]
