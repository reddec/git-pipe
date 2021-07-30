# Docker Compose

> tested on docker-compose 1.27

* Deploys all services.
* All ports in `ports` directive will be linked as sub-domains
* Each service with at least one port supports an optional `domainname` attribute which overrides sub-domain. Default is
  service name.
* First services with attribute `x-root: yes` or with name `www`, `web`, `gateway` will be additionally exposed without
  sub-domain.
* All exposed ports will be additionally exposed as sub-sub-domain with port name as the name.
* Volumes automatically backup-ed and restored (see Backup)
* Root port for service picked by the same rules as for [docker](#docker)

Domains will be generated as> `<port?>.<x-domain|service>.<x-domain|project>.<root-domain>`
and `<x-domain|project>.<root-domain>` points to `<first x-root: true|www|web|gateway>`

## Minimal example:

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

Root domain: `mini.localhost` points to `web` service to internal port `80` (the first service with name `web`, first
port in array)

## Override everything example

```yaml
version: '3'
services:
  web:
    domainname: index
    image: nginx
    ports:
      - 8080:80
      - 8081:9000
  api:
    domainname: echo
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

Root domain: `super.localhost` points to `api` service to internal port `80` (the first service with `x-root: yes`,
first port in array)
