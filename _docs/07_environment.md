# Environment variables

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


## docker

Trivial: just use environment variables as-is.

## docker-compose

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
