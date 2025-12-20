# ktl syscalls helper image: minimal environment with strace and /bin/sh
FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        strace \
        iproute2 \
        ca-certificates \
        procps \
    && rm -rf /var/lib/apt/lists/*

# Default command keeps the helper container alive until ktl execs into it.
ENTRYPOINT ["/bin/sh", "-c", "trap 'exit 0' TERM INT; while true; do sleep 3600; done"]
