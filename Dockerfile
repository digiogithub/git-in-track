# Runtime image for gintrack — see docs/09-ci-cd-and-releases.md, section 10.
#
# This Dockerfile is built by GoReleaser (the `dockers:` block in
# .goreleaser.yaml). GoReleaser's build context already holds the cross-compiled
# `gintrack` binary for the target architecture — the same binary the release
# archives carry, with the web app embedded through go:embed — so there is no
# compilation here and no Go or Node toolchain in the image.
#
# `docker build .` at the repository root therefore fails: the binary is not in
# the context. To reproduce the release image locally run `make release-snapshot`,
# which runs the same hooks (WASM core, then the Vite bundle, then the binary)
# before building the image.

FROM alpine:3.21

# ca-certificates: HTTPS git remotes for `gintrack sync`.
# git:             the optional system-git backend of internal/gitops.
RUN apk add --no-cache ca-certificates git \
    && adduser -D -H -u 10001 gintrack \
    && mkdir -p /work

COPY gintrack /usr/local/bin/gintrack
COPY LICENSE README.md /usr/share/doc/gintrack/

# The companion writes its configuration (and the generated bearer token) under
# $XDG_CONFIG_HOME. Pointing it at a world-writable path is what lets the image
# also run as `--user "$(id -u):$(id -g)"`, which is how the container is allowed
# to write to a repository mounted from the host. Container configuration is
# ephemeral by design: the mounted working tree is the only source of truth.
ENV XDG_CONFIG_HOME=/tmp/gintrack

USER 10001
WORKDIR /work
EXPOSE 7317

ENTRYPOINT ["/usr/local/bin/gintrack"]

# Binding to 0.0.0.0 is required for a published port to reach the process, and
# the port mapping — not the bind address — is what controls exposure, so publish
# it on 127.0.0.1 (see the documented `docker run` in docs/09 section 10).
# The server refuses a non-loopback bind without authentication, so a bearer
# token is generated at start and printed in the container log unless
# GINTRACK_TOKEN supplies one.
CMD ["serve", "--bind", "0.0.0.0", "--no-open", "--repo", "/work"]
