# DNS

git-pipe uses domain-based routing system which means that all exposed deployed containers will be externally accessible
by unique domain.

To support automatic TLS certificates and DNS routing allocated domains should be routed by the DNS provider. It could
be done in several ways:

1. Wildcard `*` sub-domain pointed to the git-pipe node with the sub-domain as root domain in git-pipe. For example: for
   wildcard domain `*.apps.mydomain.com`, git-pipe should be launched with flag `-d apps.mydomain.com`
2. With selected DNS providers it's possible to register domains automatically:
   use flag `-p, --provider, $PROVIDER` and provider-specific flags.

## Supported providers

### Cloudflare

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
  [Cloudflare API token](https://dash.cloudflare.com/profile/api-tokens)