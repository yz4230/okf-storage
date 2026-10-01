# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.27 AS build
WORKDIR /src

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    go mod download

ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=bind,target=. \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/okf-storage . \
 && mkdir -p /out/home/.okf-storage/bundle

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/okf-storage /okf-storage
# The bundle lives at the default --dir. A named volume mounted there
# inherits this ownership, so the nonroot user can write to it.
COPY --from=build --chown=nonroot:nonroot /out/home/.okf-storage /home/nonroot/.okf-storage
EXPOSE 8080
ENTRYPOINT ["/okf-storage"]
CMD ["mcp", "--addr", ":8080"]
