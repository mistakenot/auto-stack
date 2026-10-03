# Clean Ubuntu 24.04 with only what a typical dev box already has: curl, git,
# tar and the docker CLI + compose plugin (for `docker compose config`; the
# daemon comes with the package but is never started). jq is used by the
# assertions only — the bootstrap script itself must not depend on it.
FROM ubuntu:24.04

RUN apt-get update -qq \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends \
        ca-certificates curl git tar jq docker.io docker-compose-v2 \
    && rm -rf /var/lib/apt/lists/*

# Run as the stock non-root user: bootstrap installs into ~/.local/bin, no sudo.
USER ubuntu
WORKDIR /home/ubuntu
RUN git config --global user.name "Bootstrap Test" \
    && git config --global user.email "bootstrap-test@example.com" \
    && git config --global init.defaultBranch main
