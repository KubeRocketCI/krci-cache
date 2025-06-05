FROM alpine:3.22.0

# Set metadata labels
LABEL org.opencontainers.image.title="krci-cache" \
      org.opencontainers.image.description="KubeRocketCI component to cache pipeline artifacts" \
      org.opencontainers.image.vendor="KubeRocketCI" \
      org.opencontainers.image.source="https://github.com/KubeRocketCI/krci-cache"

# Install required packages with version pinning
RUN apk add --no-cache \
    tar=1.35-r3 \
    rsync=3.4.1-r0 \
    && rm -rf /var/cache/apk/*

# Copy the pre-built binary from dist folder
ARG TARGETARCH=amd64
COPY dist/krci-cache-${TARGETARCH} /usr/local/bin/krci-cache

# Ensure binary is executable
RUN chmod +x /usr/local/bin/krci-cache

# Create non-root user
RUN addgroup -g 1001 -S appuser && \
    adduser -u 1001 -S appuser -G appuser

# Run as non-root user
USER 1001

# Expose the default port
EXPOSE 8080

# Run the application
CMD ["/usr/local/bin/krci-cache"]
