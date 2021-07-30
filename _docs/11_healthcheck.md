# Health check

git-pipe supports health checks in single Dockerfile repositories. It will not route traffic to the service until
container will become healthy.

See [how to define health check in Dockerfile](https://docs.docker.com/engine/reference/builder/#healthcheck).

Example for common HTTP service:

```
FROM my-service
EXPOSE 80
# ....
HEALTHCHECK --interval=3s CMD curl -f http://localhost:80/health || exit 1
# ...
```