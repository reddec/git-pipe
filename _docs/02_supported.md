# Supported repo types

## docker-compose

Requires docker-compose.yaml or docker-compose.yaml file in the root directory.
See [specific configuration](#docker-compose) details;

Flow:

- `build` equal to `docker-compose build`
- `start` equal to `docker-compose up`

## docker

Requires Dockerfile in the root directory. Will be executed as-is.

Flow:

- `build` equal to `docker build`
- `start` equal to `docker run`