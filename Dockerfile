FROM golang:1.24.3-bookworm AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./

RUN go build -o /app/main -ldflags '-s -w' main.go


FROM debian:bookworm-slim

RUN groupadd -g 1000 nonroot && useradd -u 1000 -g 1000 nonroot

USER nonroot:nonroot
WORKDIR /app

COPY --chown=nonroot:nonroot --from=build /app/main /app/calsrecoil

ENTRYPOINT ["/app/calsrecoil"]
