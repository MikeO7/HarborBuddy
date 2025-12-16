## 2025-02-27 - [Unsanitized Hostname Check]
**Vulnerability:** The `isSelf` function relied on `strings.HasPrefix(containerID, hostname)` without validating if `hostname` was empty. An empty hostname causes `HasPrefix` to return true for any string, potentially misidentifying all containers as "self".
**Learning:** Functions relying on external system state (like `os.Hostname()`) must validate that the state is suitable for use before applying it to logic. "Defensive programming" requires anticipating empty or unexpected return values even from system calls.
**Prevention:** Always validate inputs from system calls (`os.Hostname`, `os.Getenv`) before using them in security-critical or logic-critical comparisons. Added explicit check: `if hostname != ""`.
