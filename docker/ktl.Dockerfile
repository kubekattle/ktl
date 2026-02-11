# syntax=docker/dockerfile:1

# Minimal runtime image that contains only the ktl binary.
#
# The CI workflow builds `bin/ktl-linux-{amd64,arm64}` into this build context
# and then `docker buildx` selects the correct one via TARGETARCH.

ARG TARGETARCH

FROM gcr.io/distroless/static-debian12:nonroot

ARG TARGETARCH
COPY bin/ktl-linux-${TARGETARCH} /usr/local/bin/ktl

ENTRYPOINT ["/usr/local/bin/ktl"]
