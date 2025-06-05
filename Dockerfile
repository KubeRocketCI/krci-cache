FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

# Set metadata labels
LABEL org.opencontainers.image.title="krci-cache" \
      org.opencontainers.image.description="KubeRocketCI component to cache pipeline artifacts" \
      org.opencontainers.image.vendor="KubeRocketCI" \
      org.opencontainers.image.source="https://github.com/KubeRocketCI/krci-cache"

# Install required packages
RUN microdnf -y update && \
    microdnf -y --nodocs install tar rsync shadow-utils && \
    microdnf clean all && \
    rm -rf /var/cache/yum

# Copy the pre-built binary from dist folder
ARG TARGETARCH=amd64
COPY dist/krci-cache-${TARGETARCH} /usr/local/bin/krci-cache

# Ensure binary is executable
RUN chmod +x /usr/local/bin/krci-cache

# Create non-root user
RUN groupadd -r appuser && useradd -r -g appuser -u 1001 appuser

# Run as non-root user
USER 1001

# Expose the default port
EXPOSE 8080

# Run the application
CMD ["/usr/local/bin/krci-cache"]
