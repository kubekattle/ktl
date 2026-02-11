FROM alpine:3.20

# This fixture is a *workload* to run under `ktl build` with a strict sandbox
# policy. It doesn't prove the sandbox by itself (the sandbox wraps the ktl
# process, not BuildKit's build container), but it provides a stable context
# you can use while validating sandbox policy changes.

RUN apk add --no-cache ca-certificates
RUN echo "sandbox-strict fixture built OK"

