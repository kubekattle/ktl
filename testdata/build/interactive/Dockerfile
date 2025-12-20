FROM alpine:3.20

WORKDIR /workspace

# Seed the filesystem so the interactive shell has something to inspect.
RUN echo "debug-me" > /workspace/README && mkdir -p /workspace/cache && touch /workspace/cache/hotfile

# Intentional failure to drive the interactive shell. Edit the failing command when you drop in.
RUN echo "About to fail so you can debug the layer" && exit 42
