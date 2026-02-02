FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /usr/local/bin/cbox ./cmd/cbox

FROM gcr.io/distroless/static-debian12

COPY --from=builder /usr/local/bin/cbox /usr/local/bin/cbox

ENTRYPOINT ["cbox"]
