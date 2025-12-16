## 2025-02-27 - [Unsanitized Hostname Check]
**Vulnerability:** The `isSelf` function relied on `strings.HasPrefix(containerID, hostname)` without validating if `hostname` was empty. An empty hostname causes `HasPrefix` to return true for any string, potentially misidentifying all containers as "self".
**Learning:** Functions relying on external system state (like `os.Hostname()`) must validate that the state is suitable for use before applying it to logic. "Defensive programming" requires anticipating empty or unexpected return values even from system calls.
**Prevention:** Always validate inputs from system calls (`os.Hostname`, `os.Getenv`) before using them in security-critical or logic-critical comparisons. Added explicit check: `if hostname != ""`.

## 2025-12-16 - [Ignored TLS Configuration]
**Vulnerability:** The configuration allowed setting `tls: true`, but the Docker client initialization (`NewClient`) only accepted a host string, completely ignoring the TLS setting. This could lead to users believing their connection is encrypted when it is not (if using TCP).
**Learning:** Having configuration fields that are not wired up to the implementation is a dangerous pattern ("Security Theater"). It's easy for the config struct and the initialization logic to drift apart.
**Prevention:** Pass the entire configuration struct (or a dedicated sub-struct) to constructors (e.g., `NewClient(config.DockerConfig)`) instead of individual arguments. This ensures that as the config evolves, the constructor has access to all settings without changing the signature.
