# HarborBuddy Dependency Version Report

Generated: $(date)

## Build Environment
- Go Version Required: 1.24
- Dockerfile Base: golang:1.24-alpine
- CI/CD Go Version: 1.24

## All Versions Status

### ✅ Direct Dependencies (All Latest)
```
github.com/docker/docker      v28.5.2    ✅ Latest (Security patched)
github.com/rs/zerolog         v1.34.0    ✅ Latest
github.com/spf13/pflag        v1.0.10    ✅ Latest
gopkg.in/yaml.v3              v3.0.1     ✅ Latest
```

### ✅ Critical Indirect Dependencies (All Latest)
```
github.com/Microsoft/go-winio v0.6.2     ✅ Latest
golang.org/x/sys              v0.38.0    ✅ Latest
golang.org/x/net              v0.47.0    ✅ Latest
golang.org/x/text             v0.31.0    ✅ Latest
golang.org/x/crypto           v0.45.0    ✅ Latest
golang.org/x/sync             v0.18.0    ✅ Latest
google.golang.org/grpc        v1.77.0    ✅ Latest
google.golang.org/protobuf    v1.36.10   ✅ Latest
```

## Security Status
🔒 **CVE-2025-54410**: RESOLVED ✅
- Vulnerability: Moby firewalld network isolation issue
- Severity: LOW (CVSS 3.3)
- Fix: Upgraded github.com/docker/docker to v28.5.2

## Verification Commands

Check versions:
```bash
go list -m all | grep docker
go list -m all | grep zerolog
go list -m all | grep pflag
```

Check for updates:
```bash
go list -m -u all | grep "\["
```

Update all:
```bash
go get -u ./...
go mod tidy
```

## Status: ALL DEPENDENCIES UP TO DATE ✅
