FROM alpine:3.14
VOLUME /app/backups /app/repos /app/ssl
WORKDIR /app
EXPOSE 80 443
ENV BIND=0.0.0.0:80 DOMAIN=localhost BACKUP=file:///app/backups
RUN apk add --no-cache git docker-compose openssl openssh-client && \
    mkdir -p /root/.ssh && \
    echo -e 'Host *\n\tUserKnownHostsFile=/dev/null\n\tStrictHostKeyChecking no' > /root/.ssh/config && \
    chmod 400 /root/.ssh
ADD git-pipe ./git-pipe
ENTRYPOINT ["/app/git-pipe"]