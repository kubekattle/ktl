FROM alpine:3.20
RUN --mount=type=cache,target=/var/cache/apk apk add --no-cache curl ca-certificates
ENV HEALTHCHECK_URL=https://example.com
CMD ["/bin/sh", "-c", "curl --version >/dev/null"]
