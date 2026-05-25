# rce-mcp

An HTTP MCP server that executes commands inside its Docker container.

## Quickstart

```sh
just docker-build
docker run --rm -p 3000:3000 \
  -e RCE_MCP_TOKEN='change-me' \
  -v "$PWD:/workspace" \
  rce-mcp
```

MCP endpoint: `http://localhost:3000/mcp`

Health check: `GET http://localhost:3000/healthz`

## Authentication

Token authentication is enabled by default. Send:

```txt
Authorization: Bearer change-me
```

Configuration can be supplied by CLI flags or environment variables:

```sh
rce-mcp --token change-me
RCE_MCP_TOKEN=change-me rce-mcp
```

To disable built-in auth behind a trusted authenticating proxy:

```sh
rce-mcp --auth-mode none
# or
RCE_MCP_AUTH_MODE=none rce-mcp
```

Do not expose this server to untrusted networks without authentication.

## Tools

### `execute_command`

Execute a program with arguments.

```json
{
  "program": "python3",
  "args": ["-c", "print('hello')"],
  "stdin": "",
  "timeout_ms": 30000,
  "output_limit_bytes": 1048576
}
```

### `execute_bash`

Execute a Bash script.

```json
{
  "script": "pwd && ls -la",
  "stdin": "",
  "timeout_ms": 30000,
  "output_limit_bytes": 1048576
}
```

Both tools return structured results:

```json
{
  "exit_code": 0,
  "stdout": "...",
  "stderr": "...",
  "stdout_truncated": false,
  "stderr_truncated": false,
  "timed_out": false,
  "duration_ms": 123
}
```

## Execution model

- The server listens on `0.0.0.0:3000` by default.
- Commands always start in `/workspace`.
- Commands run inside the container, not on the Docker host.
- The image runs as non-root user `rce:rce` with UID/GID `1000`.
- Runtime package installation is not supported by default; extend the image if you need more tools.
- Command process state is not preserved between tool calls.
- The server controls the command environment; clients cannot provide environment variables.

Default limits:

- timeout: 30s default, 5m max
- captured output: 1 MiB default per stream, 10 MiB max per stream
- stdin: 10 MiB max
- concurrent commands: 4

## Included runtime tools

The Docker image includes common command-line tools such as Bash, coreutils, curl, git, jq, make, Python 3, ripgrep, tar, gzip, and unzip. It does not include the Go toolchain.
