FROM alpine:3.20 AS build
RUN printf "artifact:%s" "$(date +%s)" > /output.txt

FROM busybox:1.36
COPY --from=build /output.txt /artifact/output.txt
CMD ["/bin/sh", "-c", "cat /artifact/output.txt"]
