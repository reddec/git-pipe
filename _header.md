# GIT-PIPE

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
7. (optional) Registers [DNS](#dns)
8. (optional) Generates [TLS certificates](#run) by Let's Encrypt HTTP-01 ACME
9. (background) Regularly creates [backup](#backup)
10. Starts from (1) in case something changes in repo

## Minimal working example

For installation from binaries:

    git-pipe https://github.com/kassambara/wordpress-docker-compose.git

Or for docker installation:

    docker run -p 127.0.0.1:8080:80 -v /var/run/docker.sock:/var/run/docker.sock reddec/git-pipe https://github.com/kassambara/wordpress-docker-compose.git

Where:

* `-p 127.0.0.1:8080:80` - docker instruction to expose port 8080 to localhost
* `-v /var/run/docker.sock:/var/run/docker.sock` - expose docker control socket to git-pipe
* `https://github.com/kassambara/wordpress-docker-compose.git` - repo to pull and build (literally I picked just random
  one. Could be several repos)

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
* [ ] authorization: by JWT/by token/by external oauth for requests for the embedded router
* [ ] support dynamic reconfiguration (over API/by file watch + signal)
* [ ] support GitHub-like webhooks

