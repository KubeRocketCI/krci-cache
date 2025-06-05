---
name: Bug report
about: Create a report to help us improve
title: ''
labels: ''
assignees: ''

---

**Describe the bug**
A clear and concise description of what the bug is.

**To Reproduce**

#### What did you do?

<!-- A clear and concise description of the steps you took (or insert a code snippet). -->

#### What did you expect to see?

<!-- A clear and concise description of what you expected to happen (or insert a code snippet). -->

#### What did you see instead? Under which circumstances?

<!-- A clear and concise description of what ACTUALLY happened (or insert a code snippet). -->

**Environment Information:**

<!-- Please fill out the following information -->

- **krci-cache version**: [e.g., v1.0.0, commit hash, or "latest"]
- **Deployment type**: [e.g., "Docker", "Kubernetes", "binary"]
- **Operating System**: [e.g., "Ubuntu 20.04", "macOS", "Windows"]
- **Go version** (if building from source):

`$ go version`

<!-- If applicable, insert the output of `go version` here -->

**Configuration:**

<!-- Please provide your configuration (sanitize any sensitive data) -->

```bash
UPLOADER_HOST=
UPLOADER_PORT=
UPLOADER_DIRECTORY=
UPLOADER_UPLOAD_CREDENTIALS=[REDACTED]
```

**Request Details:**

<!-- If the issue is related to upload/download requests, please provide: -->

- **Request method**: [GET, POST, DELETE]
- **Request path**: [e.g., "/upload", "/delete", "/path/to/file"]
- **File type/size**: [e.g., "tar.gz, 5GB", "binary file, 100MB"]
- **curl command** (sanitize credentials):

```bash
# Example (remove sensitive data):
curl -u username:password -F path=example.txt -X POST -F file=@/tmp/example.txt http://localhost:8080/upload
```

**Screenshots**
If applicable, add screenshots to help explain your problem.

**Logs**
If applicable, add relevant log output from krci-cache:

```bash
[Log output here]
```

**Additional context**
Add any other context about the problem here.
