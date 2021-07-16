# Authorization

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

## Generate tokens

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