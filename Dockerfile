FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /anubis ./cmd/anubis

FROM gcr.io/distroless/static:nonroot
COPY --from=build /anubis /anubis
ENTRYPOINT ["/anubis"]