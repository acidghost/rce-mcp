# Use one non-root runtime user

RCE MCP runs both the HTTP-facing MCP server and submitted Commands as a single non-root user created in the runtime image. This avoids running the server as root and avoids introducing a privileged user-switching helper; Docker-level configuration can still change the container user outside the application if an operator accepts that trade-off.
