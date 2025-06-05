# Security Policy

## Supported Versions

The KubeRocketCI Cache (krci-cache) project maintains release branches for the most recent releases. Security fixes may be backported to supported versions, depending on severity and feasibility.

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| main    | :white_check_mark: |

Please refer to the [releases page](https://github.com/KubeRocketCI/krci-cache/releases) for details on current supported versions.

## Security Considerations

krci-cache handles file uploads and downloads, which introduces specific security considerations:

### Authentication & Authorization
- Always deploy krci-cache behind proper authentication
- Use strong credentials via `UPLOADER_UPLOAD_CREDENTIALS`
- Never expose krci-cache directly to the internet without protection
- Consider implementing additional authorization layers for production use

### File Upload Security
- krci-cache implements protections against path traversal attacks
- Tar.gz extraction includes safety checks against zip bombs and malicious archives
- Size limits are enforced for tar.gz files (2GB per file, 8GB total)
- Regular file uploads have no built-in size limits - implement external controls as needed

### Network Security
- Use HTTPS/TLS in production deployments
- Consider network policies and firewall rules
- Deploy in a secure network environment

## Reporting a Vulnerability

We take security vulnerabilities seriously. If you discover a security issue in krci-cache, please report it responsibly.

### How to Report

**DO NOT** create a public GitHub issue for security vulnerabilities.

Instead, please report security vulnerabilities through one of these methods:

1. **GitHub Security Advisory** (Preferred): Use the "Security" tab in this repository to create a private security advisory
2. **Direct Contact**: Contact the KubeRocketCI maintainers through the [official community channels](https://docs.kuberocketci.io/community/)

### What to Include

When reporting a security vulnerability, please include:

- A clear description of the vulnerability
- Steps to reproduce the issue
- Potential impact assessment
- Suggested fix or mitigation (if you have one)
- Your contact information for follow-up

### Response Timeline

- **Acknowledgment**: We will acknowledge receipt of your report within 48 hours
- **Initial Assessment**: We will provide an initial assessment within 5 business days
- **Resolution**: We aim to resolve security issues as quickly as possible, depending on complexity

### Disclosure Policy

- We request that you do not publicly disclose the vulnerability until we have had a chance to address it
- We will coordinate with you on the timing of any public disclosure
- We will credit you for the discovery (unless you prefer to remain anonymous)

## Security Best Practices

When deploying krci-cache:

1. **Use Strong Authentication**: Set secure credentials and consider implementing additional authentication layers
2. **Network Security**: Deploy behind firewalls, use VPNs, and implement network policies
3. **Regular Updates**: Keep krci-cache updated to the latest version
4. **Monitor Logs**: Implement logging and monitoring for suspicious activity
5. **Resource Limits**: Implement appropriate disk space and upload size limits
6. **Backup Strategy**: Implement secure backup and recovery procedures

## Security Updates

Security updates will be:
- Clearly marked in release notes
- Announced through KubeRocketCI community channels
- Applied to supported versions when feasible

For the latest security information, monitor:
- [Release notes](https://github.com/KubeRocketCI/krci-cache/releases)
- [KubeRocketCI documentation](https://docs.kuberocketci.io/)
- [KubeRocketCI community channels](https://docs.kuberocketci.io/community/) 