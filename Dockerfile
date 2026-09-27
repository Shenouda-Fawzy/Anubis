# Build stage. The toolchain version must satisfy the `go` directive in go.mod.
FROM golang:1.26 AS build
WORKDIR /src

# Dependencies first so source edits do not invalidate the module cache layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Fully static, so the binary runs in a distroless image with no libc. -trimpath
# keeps build paths out of the binary; ldflags sets the version reported in logs.
ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /anubis ./cmd/anubis

# Runtime stage. distroless/static:nonroot includes CA certificates, which are
# required for the HTTPS calls to GitHub and the model provider. Keep them: a
# container without them fails every review with an x509 error.
FROM gcr.io/distroless/static:nonroot
COPY --from=build /anubis /anubis
USER nonroot:nonroot
ENTRYPOINT ["/anubis"]
