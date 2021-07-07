# Installation

## Requirements

* `docker`
* `docker-compose`
* `git`
* `openssl` - for backup en(de)cryption

During the first deployment, the following images will be downloaded automatically from docker repository

* busybox

## Pre-built binary

Download binary for your OS and arch from [github releases](https://github.com/reddec/git-pipe/releases/latest).

## Docker

Versions

- `reddec/git-pipe:<version>` - all-in-one image, Alpine based
- `reddec/git-pipe:<version>-light` - without docker-compose

To download the latest version use:

    docker pull reddec/git-pipe:latest

## Debian/Ubuntu installation

Download and install required .deb file from [github releases](https://github.com/reddec/git-pipe/releases/latest).

**It is highly recommended** to install [docker](https://docs.docker.com/engine/install/ubuntu/) and
[docker-compose](https://docs.docker.com/compose/install/) from the official Docker repository instead of APT. APT repos could be very outdated.

