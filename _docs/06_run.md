# Run

## As binary

    git-pipe [flags..] <repo, ...>

See [usage](#usage) for a list of all available flags.

**Localhost example:**

    git-pipe run https://github.com/kassambara/wordpress-docker-compose.git

**Expose to the public:**

    git-pipe run -b 0.0.0.0:8080 https://github.com/kassambara/wordpress-docker-compose.git

**Public and with Let's Encrypt certificates:**

    git-pipe run --auto-tls https://github.com/kassambara/wordpress-docker-compose.git

`--auto-tls` implies binding to `0.0.0.0:443` and automatic certificates by HTTP-01 ACME protocol.

The node should be accessible from the public internet by 443 port and routed by the domain name. Generally, there are
two universal methods of how to route traffic from the unknown amount of domains to the machine:

1. Route wildcard `*` sub-domain to the node and use the sub-domain as root domain in git-pipe. For example: for
   wildcard domain `*.apps.mydomain.com`, git-pipe should be launched with flag `-d apps.mydomain.com`
2. Use automatic DNS registration from [providers](#supported-providers)

## As docker

Version:

- `reddec/git-pipe:<version>` - all-in-one image, Alpine based
- `reddec/git-pipe:<version>-light` - without docker-compose

**Basic**

`docker run -p 80:80 -v /var/run/docker.sock:/var/run/docker.sock reddec/git-pipe run <flags same as for bin>`

**Expose to the public with TLS**

It's better to have **wildcard** certificate.

In `./certs` should be file `server.key` and `server.crt`.

`docker run -p 443:443 -v /var/run/docker.sock:/var/run/docker.sock -v $(pwd)/certs:/app/ssl reddec/git-pipe run --tls <flags same as for bin>`

**Automatic TLS**

Uses Let's Encrypt ACME HTTP-01 protocol.

`docker run -p 443:443 -v /var/run/docker.sock:/var/run/docker.sock reddec/git-pipe run --auto-tls <flags same as for bin>`

**Private repos**

Feel free to mount SSH socket:

`docker run -p 80:80 -v /var/run/docker.sock:/var/run/docker.sock -v $SSH_AUTH_SOCK:/ssh-agent -e SSH_AUTH_SOCK=/ssh-agent reddec/git-pipe run ...`

By default, SSH will be used without strict host checking. To harden pulling you may mount your own config
to `/root/.ssh/config`.

### Volumes

`/app/backups` - default directory for backups. Will not be used in case of non-file (ex: S3) backup. Without S3 it
makes sense to persist this volume.

`/app/repos` - default directory for cloned repository. It is not critical to persist this volume because git-pipe can
re-download repos anytime.

`/app/ssl` - default directory for certificates. In the case of `auto-tls` it will be used to cache keys and certs, so I
**highly recommended** to persist this volume to prevent hitting rate-limit from Let's Encrypt.

In case you are using your certificates, you should them as `server.key` and `server.crt` and you may mount them in
read-only mode.