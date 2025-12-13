FROM alpine:3.20 AS builder
RUN printf "jobs" > /opt/payload.txt

FROM busybox:1.36 AS runner
COPY --from=builder /opt/payload.txt /opt/payload.txt
CMD ["/bin/sh", "-c", "cat /opt/payload.txt"]
