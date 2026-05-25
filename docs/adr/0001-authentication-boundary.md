# Static token is the default authentication boundary

RCE MCP requires a static bearer token for HTTP requests by default because it exposes arbitrary command execution. Authentication can be explicitly disabled only when the server is deployed behind another trusted authentication boundary, such as an authenticating proxy; this keeps local/simple deployments possible without making unauthenticated command execution the default. The authentication mode is explicit: token authentication is the default, and `none` is an opt-in mode.
