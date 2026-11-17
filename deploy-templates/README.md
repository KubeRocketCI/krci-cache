# krci-cache

![Version: 0.6.0-SNAPSHOT](https://img.shields.io/badge/Version-0.6.0--SNAPSHOT-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.6.0-SNAPSHOT](https://img.shields.io/badge/AppVersion-0.6.0--SNAPSHOT-informational?style=flat-square)
[![Artifact HUB](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/epmdedp)](https://artifacthub.io/packages/search?repo=epmdedp)

A Helm chart for KubeRocketCI Cache, an HTTP cache for Tekton pipeline artifacts

## Additional Information

The krci-cache Helm chart deploys an HTTP cache for Tekton pipeline artifacts.
Pipeline tasks upload dependencies and build outputs to the cache and restore them in later runs, which reduces build times.
The KubeRocketCI `edp-tekton` chart installs it as the `krci-cache` subchart.

**Homepage:** <https://docs.kuberocketci.io>

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| epmd-edp | <SupportEPMD-EDP@epam.com> | <https://solutionshub.epam.com/solution/kuberocketci> |
| sergk |  | <https://github.com/SergK> |

## Source Code

* <https://github.com/KubeRocketCI/krci-cache>

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Pod affinity. |
| cacheSize | string | `"5Gi"` | Defines size of the Persistent Volume that is used for cache. |
| fullnameOverride | string | `""` |  |
| image.pullPolicy | string | `"IfNotPresent"` | Defines the policy with which pod will request its image. |
| image.repository | string | `"epamedp/krci-cache"` | krci-cache container image. Add a registry prefix to use a mirror or a private registry. |
| image.tag | string | `""` | Overrides the image tag whose default is the chart appVersion. |
| imagePullSecrets | list | `[]` | Specifies secrets for pulling Docker images. |
| initContainers.repository | string | `"busybox"` | Defines the repository. |
| initContainers.tag | string | `"1.36.1"` | InitContainer image. |
| nameOverride | string | `""` |  |
| nodeSelector | object | `{}` | Node labels for krci-cache pod assignment. |
| podAnnotations | object | `{}` | Annotations to be added to krci-cache pods. |
| podSecurityContext | object | `{"fsGroup":1001,"fsGroupChangePolicy":"OnRootMismatch","runAsGroup":1001,"runAsNonRoot":true,"runAsUser":1001,"seccompProfile":{"type":"RuntimeDefault"}}` | Specifies privilege and access control configurations for krci-cache pods. runAsUser and runAsGroup set the cache volume owner applied by the fix-permissions init container. Default 1001 matches the image USER. |
| resources | object | `{}` | krci-cache pod resource requests and limits. |
| securityContext | object | `{}` | Security context to be added to krci-cache pods. |
| service | object | `{"name":"krci-cache","port":8080,"type":"ClusterIP"}` | krci-cache service configurations. |
| tolerations | list | `[]` | Node tolerations for pod scheduling to nodes with taints. |
