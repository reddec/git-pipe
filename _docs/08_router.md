# Router

Router (proxy) provides reverse-proxy concept.

`-D, --dummy, $DUMMY` disables router completely. Could be useful for services deployed without HTTP services.

## Domain routing

By-default, each repo and service deployed as separated domain. Root domain can be defined in `-d,--domain,$DOMAIN`.

## Path routing

In case multiple domains is not an option the path-based routing can be useful. Enabled by
flag `-P,--path-routing,$PATH_ROUTING` which means that services will be under the same domain, but under different path
prefixes. In this mode, `--domain` flag will not be used for services name,
however, it still required for automatic TLS. 

