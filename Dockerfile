FROM alpine:3.23

# Set metadata labels
LABEL org.opencontainers.image.title="krci-cache" \
      org.opencontainers.image.description="KubeRocketCI component to cache pipeline artifacts" \
      org.opencontainers.image.vendor="KubeRocketCI" \
      org.opencontainers.image.source="https://github.com/KubeRocketCI/krci-cache"

# Alpine keeps only the latest build of a package per branch: pin minors (~=), not builds, and bump them with the FROM branch.
# apk upgrade brings base image packages up to the branch's latest builds.
RUN apk upgrade --no-cache && \
    apk add --no-cache \
    tar~=1.35 \
    rsync~=3.5

# Copy the pre-built binary from dist folder
ARG TARGETARCH
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
