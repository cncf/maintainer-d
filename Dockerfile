FROM golang:1.24 AS build
WORKDIR /src

# Cache module downloads and build artifacts with BuildKit
# 1) copy only go.mod/sum first to maximize cache hits
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# 2) copy the rest of the source and build using cached mounts
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o /bootstrap ./cmd/bootstrap && \
    go build -o /maintainerd ./main.go && \
    go build -o /sync ./cmd/sync

FROM gcr.io/distroless/base-debian12 AS maintainerd
COPY --from=build /bootstrap /usr/local/bin/bootstrap
COPY --from=build /maintainerd /usr/local/bin/maintainerd
ENTRYPOINT ["/usr/local/bin/maintainerd"]

FROM gcr.io/distroless/base-debian12 AS sync
COPY --from=build /sync /usr/local/bin/sync
ENTRYPOINT ["/usr/local/bin/sync"]
