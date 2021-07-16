# GIT-PIPE

![logo](_docs/logo.png?raw=true)


Hassle-free minimal CI/CD for git repos for docker-based projects.

Features:

* zero configuration for repos by default
* automatic encrypted backup and recover via different providers including plain files or S3
* optional automatic TLS by Let's Encrypt
* optional automatic domain registration by supported providers
* minimal additional overhead
* multiple repos at once without ports conflicts

## How does it work

git-pipe does for you:

1. Clone/fetch remote repository
2. Detect [packaging type](#supported-repo-types)
3. Build package
4. Restore [backup](#backup) (if applicable)
5. Starts container(s)
6. Creates [proxy router](#router)
7. (optional) Registers [DNS](#supported-providers)
8. (optional) Generates [TLS certificates](#run) by Let's Encrypt HTTP-01 ACME
9. (background) Regularly creates [backup](#backup)
10. Starts from (1) in case something changes in repo

## Minimal working example

For installation from binaries:

    git-pipe run https://github.com/kassambara/wordpress-docker-compose.git

Or for docker installation:

    docker run -p 127.0.0.1:8080:80 -v /var/run/docker.sock:/var/run/docker.sock reddec/git-pipe run https://github.com/kassambara/wordpress-docker-compose.git

Where:

* `-p 127.0.0.1:8080:80` - docker instruction to expose port 8080 to localhost
* `-v /var/run/docker.sock:/var/run/docker.sock` - expose docker control socket to git-pipe
* `https://github.com/kassambara/wordpress-docker-compose.git` - repo to pull and build (literally I picked just random
  one. Could be several repos)

Check [usage section](#usage) for details.

Wait a bit to finish building and go to

* http://wordpress.wordpress-docker-compose.localhost:8080 - wordpress app
* http://phpmyadmin.wordpress-docker-compose.localhost:8080 - for phpmyadmin app



## Supported OS

* `linux` - high priority
* `darwin` - (i-wish-i-had-a-mac priority) should work...
* `windows` - (community support) maybe works, never tested but compiled

## Future goals

* [ ] zero-deps: replace OpenSSL, git, ssh and docker-compose to Go-native variants
* [ ] file config: support file-based per repo configurations
* [ ] authorization: 
  *  [x] by JWT 
  *  [ ] OIDC
* [ ] support dynamic reconfiguration (over API/by file watch + signal)
* [ ] support GitHub-like webhooks
* [ ] lazy initialization
* [x] path routing as alternative to domain-based
## Installation


### Requirements

* `docker`
* `docker-compose`
* `git`
* `openssl` - for backup en(de)cryption

During the first deployment, the following images will be downloaded automatically from docker repository

* busybox


### Pre-built binary

Download binary for your OS and arch from [github releases](https://github.com/reddec/git-pipe/releases/latest).


### Docker

Versions

- `reddec/git-pipe:<version>` - all-in-one image, Alpine based
- `reddec/git-pipe:<version>-light` - without docker-compose

To download the latest version use:

    docker pull reddec/git-pipe:latest


### Debian/Ubuntu installation

Download and install required .deb file from [github releases](https://github.com/reddec/git-pipe/releases/latest).

**It is highly recommended** to install [docker](https://docs.docker.com/engine/install/ubuntu/) and
[docker-compose](https://docs.docker.com/compose/install/) from the official Docker repository instead of APT. APT repos could be very outdated.


## Supported repo types


### docker-compose

Requires docker-compose.yaml or docker-compose.yaml file in the root directory.
See [specific configuration](#docker-compose) details;

Flow:

- `build` equal to `docker-compose build`
- `start` equal to `docker-compose up`


### docker

Requires Dockerfile in the root directory. Will be executed as-is.

Flow:

- `build` equal to `docker build`
- `start` equal to `docker run`# Docker Compose

> tested on docker-compose 1.27

* Deploys all services.
* All ports in `ports` directive will be linked as sub-domains
* Root compose file supports optional `x-domain` attribute which overrides domain prefix. Default is repo name (or FQDN)
  .
* Each service with at least one port supports an optional `x-domain` attribute which overrides sub-domain. Default is
  service name.
* First services with attribute `x-root: yes` or with name `www`, `web`, `gateway` will be additionally exposed without
  sub-domain.
* All exposed ports will be additionally exposed as sub-sub-domain with port name as the name.
* Volumes automatically backup-ed and restored (see Backup)

Domains will be generated as> `<port?>.<x-domain|service>.<x-domain|project>.<root-domain>`
and `<x-domain|project>.<root-domain>` points to `<first x-root: true|www|web|gateway>`


### Minimal example:

```yaml
version: '3'
services:
  web:
    image: nginx
    ports:
      - 8080:80
      - 8081:9000
  api:
    image: hashicorp/http-echo
    command: -listen :80 -text "web"
    ports:
      - 8082:80
```

Repo name: github.com/example/mini

Generated mapping (root domain (`-d,--domain,$DOMAIN`) is `localhost`):

* `web.mini.localhost` - points to `web` service to internal port `80` (the first port in array)
* `80.web.mini.localhost` - same
* `9000.web.mini.localhost` - points to `web` service to internal port `9000`
* `api.mini.localhost` - points `api` service to internal port `80`
* `80.api.mini.localhost` - same

Root domain: `mini.localhost` points to `web` service to internal port `80` (the first service with name `web`, first port
in array)


### Override everything example

```yaml
version: '3'
x-domain: super
services:
  web:
    x-domain: index
    image: nginx
    ports:
      - 8080:80
      - 8081:9000
  api:
    x-domain: echo
    x-root: yes
    image: hashicorp/http-echo
    command: -listen :80 -text "web"
    ports:
      - 8082:80
```

Repo name: github.com/example/mini

Generated mapping (root domain (`-d,--domain,$DOMAIN`) is `localhost`):

* `index.super.localhost` - points to `web` service to internal port `80` (the first port in array)
* `80.index.super.localhost` - same
* `9000.index.super.localhost` - points to `web` service to internal port `9000`
* `echo.super.localhost` - points `api` service to internal port `80`
* `80.echo.super.localhost` - same

Root domain: `super.localhost` points to `api` service to internal port `80` (the first service with `x-root: yes`, first
port in array)

## Backup

For the single Dockerfile setup:

- All defined `VOLUME` section in Dockerfile will be added to the archive.

For docker-compose setup:

- All non-external, local (driver `local` or empty) volumes defined in `volumes:` section in full notation will be added
  to archive.

Backup interval defined by `-I,--backup-interval,$BACKUP_INTERVAL` and by default equal to `1h` (every 1 hour).

The default encryption is symmetric AES-256 CBC done by OpenSSL. Encryption key defined in `--backup-key,-K,$BACKUP_KEY` and
by-default equal to `git-pipe-change-me`.

Restore will be done **automatically** before the first run.


### Supported destination

Defined by `-B,--backup,$BACKUP`. Default is `file://backups`

* `file://<directory>` - archive in directory. Creates temp (`.!tmp` suffix) during backup.
* `s3://<id>:<secret>@<endpoint>/<bucket>[?params]` - upload/download to/from S3-like storage
* `<empty>` or `none` - disable backup

S3 query params:

The bucket should be created by an administrator.

* `force_path=true|false`, default `false` - force use path style for buckets. Required for Minio
* `region=<name>`, default `us-west-1` - region
* `disable_ssl=true|false`, default `false`, disable SSL for endpoint

**Example for local Minio**:

Launch minio: `docker run -p 9000:9000 minio/minio server /data`

Backup URL: `s3://minioadmin:minioadmin@127.0.0.1:9000/backups?force_path=true&disable_ssl=true`

**Example for BackBlaze (B2)**:

* Obtain id and secret from [admin panel](https://secure.backblaze.com/app_keys.htm)
* Copy information about region and bucket name [in dashboard](https://secure.backblaze.com/b2_buckets.htm)

Backup URL: `s3://<id>:<secret>@s3.<region>.backblazeb2.com/<bucket name>`

> (B2) There is some lag between backup and availability to download. Usually, it's around 2-5 minutes for me. 

## Git

git-pipe uses `git` executable so all configuration from `~/.git` is supported.

It is a good idea to generate deployment SSH keys with read-only access for production usage, however, it is not
mandatory.

## Run


### As binary

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


### As docker

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


#### Volumes

`/app/backups` - default directory for backups. Will not be used in case of non-file (ex: S3) backup. Without S3 it
makes sense to persist this volume.

`/app/repos` - default directory for cloned repository. It is not critical to persist this volume because git-pipe can
re-download repos anytime.

`/app/ssl` - default directory for certificates. In the case of `auto-tls` it will be used to cache keys and certs, so I
**highly recommended** to persist this volume to prevent hitting rate-limit from Let's Encrypt.

In case you are using your certificates, you should them as `server.key` and `server.crt` and you may mount them in
read-only mode.# Environment variables

`git-pipe` will pass the environment to the packs by prefix: where prefix is repo name (simple or FQDN - depends on setup)
in upper case with dash replaced to underscore. Passed keys will be trimmed from suffix: `TINC_BOOT_X_Y_Z` will be
passed as `X_Y_Z`.

Environment variables can be passed by system-level and/or from file `-e, --env-file path/to/file`. Env files can be defined several times.
Each next file overwrites the previous value with the same key. Latest goes system environment, which means that system's
environment variables have the highest priority.

Basic example:

* repo: https://example.com/example/my-example.git

Let's guess that the application needs a database URL which you don't want to expose in the repo. App needs variable
`DB_URL`.

By default, we need to pass it as `MY_EXAMPLE_DB_URL=something` because

- Repo name is `my-example` which converted to `MY_EXAMPLE_` prefix
- Variable name `DB_URL`

In case you used `--fqdn` you should specify the full name of repo: `MY_EXAMPLE.EXAMPLE.EXAMPLE.COM_DB_URL`.



### docker

Trivial: just use environment variables as-is.


### docker-compose

To use env variables in compose
use [variables substitution](https://docs.docker.com/compose/environment-variables/#substitute-environment-variables-in-compose-files):

```yaml
version: '3'
services:
  app:
    image: my-app:latest
    environment:
      DB_URL: "${DB_URL:-localhost}"
```

## Router

Router (proxy) provides reverse-proxy concept.

`-D, --dummy, $DUMMY` disables router completely. Could be useful for services deployed without HTTP services.


### Domain routing

By-default, each repo and service deployed as separated domain. Root domain can be defined in `-d,--domain,$DOMAIN`.


### Path routing

In case multiple domains is not an option the path-based routing can be useful. Enabled by
flag `-P,--path-routing,$PATH_ROUTING` which means that services will be under the same domain, but under different path
prefixes. In this mode, `--domain` flag will not be used for services name,
however, it still required for automatic TLS. 


## DNS

git-pipe uses domain-based routing system which means that all exposed deployed containers will be externally accessible
by unique domain.

To support automatic TLS certificates and DNS routing allocated domains should be routed by the DNS provider. It could
be done in several ways:

1. Wildcard `*` sub-domain pointed to the git-pipe node with the sub-domain as root domain in git-pipe. For example: for
   wildcard domain `*.apps.mydomain.com`, git-pipe should be launched with flag `-d apps.mydomain.com`
2. With selected DNS providers it's possible to register domains automatically:
   use flag `-p, --provider, $PROVIDER` and provider-specific flags.


### Supported providers


#### Cloudflare

Provider name: `cloudflare`

Requires API-Token for the zone in which you want to register sub-domains.

Enable by:

    -d MYDOMAIN -p cloudflare --cloudflare.api-token XXXXX

Where `MYDOMAIN` is your root domain which will be added to all
apps; `XXXXX` [Cloudflare API token](https://dash.cloudflare.com/profile/api-tokens)

> `-d, --domain, $DOMAIN <root domain name>` is theoretically optional in case you hard-coded root domains in
> manifest, but I guess it's not a common situation and should be avoided in most setups.


Options:

* `--cloudflare.ip <IP>` (`$CLOUDFLARE_IP`) - Public IP address for DNS record. If not defined - will be detected
  automatically by myexternalip.com
* `--cloudflare.proxy` (`$CLOUDFLARE_PROXY`) - Let Cloudflare proxy traffic. Implies some level of protection and
  automatic SSL between client and Cloudflare
* `--cloudflare.api-token <TOKEN>` (`$CLOUDFLARE_API_TOKEN`) -
  [Cloudflare API token](https://dash.cloudflare.com/profile/api-tokens)# Authorization

It's possible to secure exposed endpoints by JWT.

**Important:** index endpoint not secured by authorization, however, it can expose only list of deployed applications.
Use `--no-index` flag to disable index page.

Supported claims:

* `nbf` - not before
* `exp` - expiration time. Permanent if not defined
* `sub` - restrict to single group (repo) by name (or FQDN if `--fqdn` set)
* `aud` - (required) client name (will be used for statistics and probably for lock)
* `methods` - list of allowed methods: GET, POST, etc.. Case-insensitive, all methods allowed if list is empty or
  undefined.

To enable JWT authorization use `--jwt <key>` flag, where `<key>` shared key used for signing tokens.

Tokens can be (sorted by priority):

* in header `Authorization` with `Bearer` (case-insensitive) kind
* in query param `token`


**For example** you have token `eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJjbGllbnQxIn0.tj2xpg4u-IHzqXtjfpmI8QUFKQIQUrxPdCQY4JSfCWI`
 so cURL requests may be:

* in header: `curl -H 'Authorization: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJjbGllbnQxIn0.tj2xpg4u-IHzqXtjfpmI8QUFKQIQUrxPdCQY4JSfCWI' http://app.example.com/`
* in query: `curl http://app.example.com/?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJjbGllbnQxIn0.tj2xpg4u-IHzqXtjfpmI8QUFKQIQUrxPdCQY4JSfCWI`


### Generate tokens

Check `git-pipe jwt` command in [usage](#usage).

Example:

    git-pipe jwt -s changeme my-client-1

will generate and print token labeled as `my-client-1` without expiration, for all groups (repos), for all methods,
assuming that shared key is `changeme`.

Example with restriction to single group (repo):

    git-pipe jwt -s changeme -g my-app my-client-1

Example with restriction to single group (repo) and only for GET and HEAD methods:

    git-pipe jwt -s changeme -g my-app -m GET -m HEAD my-client-1

Example with expiration after 3 hours 20 minutes and 5 seconds:

    git-pipe jwt -s changeme -e 3h20m5s my-client-1

Example how to generate for multiple same clients:

    git-pipe jwt -s changeme my-client-1 my-client-2 my-client-3 my-client-4

## Usage


* `jwt` - helper to generate JWT
* `run` - (default) run git-pipe and serve repos

## jwt
```
Usage:
  git-pipe [OPTIONS] jwt [jwt-OPTIONS] [name...]

Help Options:
  -h, --help            Show this help message

[jwt command options]
      -s, --secret=     Shared JWT secret [$SECRET]
      -g, --group=      Allowed group (repo name) [$GROUP]
      -e, --expiration= Expiration time [$EXPIRATION]
      -m, --methods=    Allowed HTTP methods [$METHODS]

[jwt command arguments]
  name:                 Client names for each token will be generated

```


## run
```
Usage:
  git-pipe [OPTIONS] run [run-OPTIONS] [git-url...]

Help Options:
  -h, --help                      Show this help message

[run command options]
      -d, --domain=               Root domain, default is hostname (default:
                                  localhost) [$DOMAIN]
      -D, --dummy                 Dummy mode disables HTTP router [$DUMMY]
      -b, --bind=                 Address to where bind HTTP server (default:
                                  127.0.0.1:8080) [$BIND]
      -T, --auto-tls              Automatic TLS (Let's Encrypt), ignores bind
                                  address and uses 0.0.0.0:443 port [$AUTO_TLS]
          --tls                   Enable HTTPS serving with TLS. TLS files
                                  should support multiple domains, otherwise
                                  path-routing should be enabled. Ignored with
                                  --auto-tls' [$TLS]
          --ssl-dir=              Directory for SSL certificates and keys.
                                  Should contain server.{crt,key} files unless
                                  auto-tls enabled. For auto-tls it is used as
                                  cache dir (default: ssl) [$SSL_DIR]
          --no-index              Disable index page [$NO_INDEX]
      -P, --path-routing          Enable path routing instead of domain-based.
                                  Implicitly disables --domain [$PATH_ROUTING]
          --jwt=                  Define JWT secret and enable JWT-based
                                  authorization [$JWT]
      -n, --network=              Network name for internal communication
                                  (default: git-pipe) [$NETWORK]
      -i, --interval=             Interval to poll repositories (default: 30s)
                                  [$INTERVAL]
      -o, --output=               Output directory for clone (default: repos)
                                  [$OUTPUT]
      -B, --backup=               Backup location (default: file://backups)
                                  [$BACKUP]
      -K, --backup-key=           Backup key (default: git-pipe-change-me)
                                  [$BACKUP_KEY]
      -I, --backup-interval=      Backup interval (default: 1h)
                                  [$BACKUP_INTERVAL]
      -F, --fqdn                  Construct from URL unique FQDN based on path
                                  and domain [$FQDN]
          --graceful-shutdown=    Interval before server shutdown (default:
                                  15s) [$GRACEFUL_SHUTDOWN]
      -e, --env-file=             Environment variables files [$ENV_FILE]
      -p, --provider=[cloudflare] DNS provider for auto registration [$PROVIDER]

    Cloudflare config:
          --cloudflare.ip=        Public IP address for DNS record. If not
                                  defined - will be detected automatically by
                                  myexternalip.com [$CLOUDFLARE_IP]
          --cloudflare.proxy      Let Cloudflare proxy traffic. Implies some
                                  level of protection and automatic SSL between
                                  client and Cloudflare [$CLOUDFLARE_PROXY]
          --cloudflare.api-token= API token [$CLOUDFLARE_API_TOKEN]

[run command arguments]
  git-url:                        remote git URL to poll with optional
                                  branch/tag name after hash

```

