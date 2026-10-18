# syntax=docker/dockerfile:1
#
# The smind daemon image (ADR-0031 D5 / P11).
#
# Nothing is compiled here: the Dockerfile COPYs the release's own per-arch
# linux binary from the build context (bin/<amd64|arm64>/smind), so the image
# carries byte-for-byte the binary that checksums.txt lists. `task docker:bin`
# produces those two files for a local or CI build; the release workflow
# extracts them from the release tarballs.
#
#   task docker:bin && docker buildx build --platform linux/amd64,linux/arm64 -t smind .
#
# Run it (a password is mandatory: the image listens on 0.0.0.0, and the
# entrypoint and the daemon both refuse to start otherwise):
#
#   docker run -d -p 4648:4648 -e SMIND_PASSWORD="$(openssl rand -base64 24)" \
#     -v smind-data:/data -v "$PWD/workspaces:/workspaces" ghcr.io/spacingmind/smind
FROM debian:bookworm-slim

ARG TARGETARCH
ARG VERSION=dev
# Node 22 LTS: the GLM agent starts with `npx`.
ARG NODE_VERSION=22.23.3
# SHA-256 of node-v${NODE_VERSION}-linux-<arch>.tar.xz, pinned here (from
# https://nodejs.org/dist/v22.23.3/SHASUMS256.txt) so the download is checked
# against something that lives in this repository, not against a file fetched
# from the same host as the tarball. Bump all three together.
ARG NODE_SHA256_AMD64=df450af89261115ef9f9e3830c3eeb2cc9213b63c720b1af623cb5dcbe2e02de
ARG NODE_SHA256_ARM64=a44aeb94849a299b22df10b9e622ec2f605c2183501bc40590705131de7c740f

LABEL org.opencontainers.image.title="smind" \
      org.opencontainers.image.description="The smind daemon: agent orchestration with a web UI" \
      org.opencontainers.image.source="https://github.com/spacingmind/smind" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"

# git + openssh-client: worktrees and remotes; curl: the healthcheck; gosu + tini:
# privilege drop and PID 1; procps: pgrep for diagnostics and the smoke tests.
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates curl git gosu openssh-client procps tini xz-utils; \
    case "${TARGETARCH}" in \
        amd64) node_arch=x64; node_sha="${NODE_SHA256_AMD64}" ;; \
        arm64) node_arch=arm64; node_sha="${NODE_SHA256_ARM64}" ;; \
        *) echo "unsupported TARGETARCH '${TARGETARCH}'" >&2; exit 1 ;; \
    esac; \
    node_tar="node-v${NODE_VERSION}-linux-${node_arch}.tar.xz"; \
    cd /tmp; \
    curl --proto '=https' --tlsv1.2 -fsSLO "https://nodejs.org/dist/v${NODE_VERSION}/${node_tar}"; \
    echo "${node_sha}  ${node_tar}" | sha256sum -c -; \
    tar -xJf "${node_tar}" -C /usr/local --strip-components=1 --no-same-owner; \
    rm -f "${node_tar}"; \
    node --version; npm --version; \
    apt-get purge -y --auto-remove xz-utils; \
    rm -rf /var/lib/apt/lists/*; \
    groupadd --gid 1000 smind; \
    useradd --uid 1000 --gid 1000 --create-home --home-dir /home/smind --shell /bin/bash smind; \
    mkdir -p /data /workspaces; \
    chown smind:smind /data /workspaces

COPY --chmod=0755 bin/${TARGETARCH}/smind /usr/local/bin/smind
COPY --chmod=0755 docker/entrypoint /usr/local/bin/smind-entrypoint

ENV HOME=/home/smind \
    SMIND_HOME=/data \
    SMIND_LISTEN=0.0.0.0:4648 \
    SMIND_SUPERVISOR=docker

# State (database, token, relay config, config.yaml) lives in /data; agent
# credentials in /home/smind (.claude, .codex); repositories under /workspaces.
VOLUME ["/data"]
EXPOSE 4648

# The daemon's port follows SMIND_LISTEN; /healthz is unauthenticated.
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD sh -c 'p="${SMIND_LISTEN##*:}"; curl -fsS "http://127.0.0.1:${p:-4648}/healthz" >/dev/null'

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/smind-entrypoint"]
CMD ["smind", "serve"]
