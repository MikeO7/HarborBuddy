# Dependency Updates - Latest Versions

## Major Security Fix
✅ **CVE-2025-54410 FIXED** - Updated Docker SDK to v28.5.2

## Updated Dependencies

### Direct Dependencies
- `github.com/docker/docker`: v27.4.1 → **v28.5.2** ⭐ (Security fix)
- `github.com/rs/zerolog`: v1.33.0 → **v1.34.0** (Latest logging library)
- `github.com/spf13/pflag`: v1.0.5 → **v1.0.10** (Latest CLI flags library)
- `gopkg.in/yaml.v3`: v3.0.1 (Already latest)

### Major Indirect Dependency Updates
- `github.com/Microsoft/go-winio`: v0.4.21 → **v0.6.2**
- `golang.org/x/sys`: v0.35.0 → **v0.38.0**
- `golang.org/x/net`: v0.43.0 → **v0.47.0**
- `golang.org/x/text`: v0.28.0 → **v0.31.0**
- `golang.org/x/crypto`: v0.44.0 → **v0.45.0**
- `golang.org/x/oauth2`: v0.32.0 → **v0.33.0**
- `golang.org/x/sync`: v0.17.0 → **v0.18.0**
- `golang.org/x/mod`: v0.12.0 → **v0.30.0**
- `golang.org/x/tools`: v0.13.0 → **v0.39.0**
- `google.golang.org/grpc`: v1.75.0 → **v1.77.0**
- `google.golang.org/protobuf`: v1.36.8 → **v1.36.10**
- `cel.dev/expr`: v0.24.0 → **v0.25.1**
- `github.com/envoyproxy/go-control-plane`: v0.13.5 → **v0.14.0**
- `github.com/grpc-ecosystem/grpc-gateway/v2`: v2.27.2 → **v2.27.3**

### Other Updates
- `github.com/containerd/typeurl/v2`: v2.2.0 → v2.2.3
- `github.com/coreos/go-systemd/v22`: v22.5.0 → v22.6.0
- `github.com/creack/pty`: v1.1.18 → v1.1.24
- `github.com/godbus/dbus/v5`: v5.0.4 → v5.2.0
- `github.com/mattn/go-colorable`: v0.1.13 → v0.1.14
- `github.com/mattn/go-isatty`: v0.0.19 → v0.0.20
- `go.opentelemetry.io/auto/sdk`: v1.1.0 → v1.2.1
- `go.opentelemetry.io/proto/otlp`: v1.7.1 → v1.9.0

### New Dependencies Added
- `github.com/containerd/errdefs` v1.0.0
- `github.com/containerd/errdefs/pkg` v0.3.0
- `github.com/moby/sys/atomicwriter` v0.1.0
- `github.com/moby/sys/sequential` v0.6.0

## Total Updates: 35+ packages

## Verification
✅ All tests passing
✅ Binary builds successfully  
✅ No breaking changes
✅ Security vulnerability resolved

## Date: 2024-12-03
