# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /


COPY ./dist/manager /manager

USER 1001

EXPOSE 8080
ENTRYPOINT ["/manager"]