# Static base: no shell, no package manager. Binary must be built with CGO_ENABLED=0.
FROM gcr.io/distroless/static:nonroot

# Set metadata labels
LABEL org.opencontainers.image.title="krci-cache" \
      org.opencontainers.image.description="KubeRocketCI component to cache pipeline artifacts" \
      org.opencontainers.image.vendor="KubeRocketCI" \
      org.opencontainers.image.source="https://github.com/KubeRocketCI/krci-cache"

ARG TARGETARCH
COPY --chmod=0755 dist/krci-cache-${TARGETARCH} /usr/local/bin/krci-cache

# UID/GID 1001 owns existing cache volumes. Keep equal to the chart podSecurityContext default.
USER 1001:1001

EXPOSE 8080

CMD ["/usr/local/bin/krci-cache"]
