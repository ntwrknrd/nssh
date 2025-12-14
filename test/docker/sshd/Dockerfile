FROM alpine:3.19

RUN apk add --no-cache \
    openssh-server \
    openssh-client \
    bash \
    shadow \
    netcat-openbsd

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 22

ENTRYPOINT ["/entrypoint.sh"]
