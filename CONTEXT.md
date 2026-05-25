# RCE MCP

RCE MCP exposes command execution to MCP clients while keeping the execution target explicit and bounded.

## Language

**Execution Environment**:
The isolated place where a Command runs. A Command affects only its Execution Environment, not the host that starts or supervises the server. Commands see only environment values intentionally made available by the server inside this boundary.
_Avoid_: Host, machine, runner

**Workspace**:
The working directory where every client-requested Command starts. Filesystem access is bounded by the Execution Environment rather than by application-level path filtering.
_Avoid_: Project root, mount, repo

**Command**:
A requested operating-system-level action submitted by an MCP client for execution in the Execution Environment. Commands are independent requests; process-level state from one Command is not part of another Command. A Command may include bounded input for the process to read.
_Avoid_: Task, job, script

**Command Tool**:
An MCP tool that accepts Commands from clients. RCE MCP exposes a program-and-arguments Command Tool and a Bash Command Tool; both produce Command Results under the same Command Limits.
_Avoid_: Terminal tool, runner tool

**Bash Command Tool**:
The MCP tool that accepts a Bash script string and submits it to Bash as the Command. RCE MCP does not interpret the script; Bash owns its meaning.
_Avoid_: Shell parser, script runner

**Command Result**:
The completed outcome of a Command, including captured output, process exit status, and whether execution reached a Command Limit. A Command Result describes process outcomes, including non-zero exits and timeouts, rather than treating them as tool failures.
_Avoid_: Response, log, transcript

**Command Limit**:
A boundary applied to a Command so that execution and output remain finite. Command Limits include runtime and captured-output limits. When a runtime limit is reached, the Command and the processes it started are no longer part of the active Execution Environment.
_Avoid_: Quota, guardrail, budget

## Example dialogue

Dev: "If a Command deletes files, what can it delete?"
Domain expert: "Only files inside the Execution Environment. The host is outside the boundary."
