FROM alpine:3.20
ARG APP_NAME="ktl"
ENV APP_NAME=${APP_NAME}
COPY message.txt /etc/app/message.txt
CMD ["/bin/sh", "-c", "echo $APP_NAME && cat /etc/app/message.txt"]
