# syntax=docker/dockerfile:1.6
FROM alpine:3.20

# This demo is safe: it tries to read a marker file from the host's /tmp via a bind mount.
# If the builder is permissive and the build runs directly on the host, the marker may be readable.
# When running inside the ktl sandbox, the host's /tmp should not be visible (unless explicitly bound in),
# so the marker should be missing.
ARG MARKER_PATH=/tmp/ktl-sandbox-demo-host-marker.txt

RUN --mount=type=bind,src=/tmp,readwrite=false,target=/host-tmp \
  sh -ceu 'if [ -f "/host-tmp/$(basename "$MARKER_PATH")" ]; then echo "HOST_MARKER:present"; cat "/host-tmp/$(basename "$MARKER_PATH")"; else echo "HOST_MARKER:missing"; fi'

