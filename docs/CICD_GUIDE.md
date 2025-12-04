# CI/CD Guide for HarborBuddy

## Overview

HarborBuddy uses GitHub Actions for CI/CD with automatic multi-architecture Docker image builds.

## Supported Architectures

All Docker images are built for:
- **linux/amd64** (Intel/AMD 64-bit) - Most cloud servers, desktops
- **linux/arm64** (ARM 64-bit) - Apple Silicon, Raspberry Pi 4/5, AWS Graviton
- **linux/arm/v7** (ARM 32-bit) - Raspberry Pi 3, older ARM devices

## Workflows

### 1. CI Workflow (`.github/workflows/ci.yml`)

**Triggers**: Push to main, Pull Requests

**Jobs**:
- ✅ Run all tests
- ✅ Run go vet
- ✅ Check code formatting
- ✅ Build binary
- ✅ Test Docker build

**Does NOT push images** - just validates everything works.

### 2. Docker Build Workflow (`.github/workflows/docker-build.yml`)

**Triggers**: 
- Push to main branch
- Git tags starting with `v*`
- Pull Requests (build only, no push)

**Features**:
- Multi-architecture builds (amd64, arm64, arm/v7)
- Automatic tagging based on context
- Image caching for faster builds
- Build metadata and labels

**Image Tags**:
- **For releases** (git tag `v1.0.0`):
  - `ghcr.io/mikeo/harborbuddy:1.0.0`
  - `ghcr.io/mikeo/harborbuddy:1.0`
  - `ghcr.io/mikeo/harborbuddy:1`
  - `ghcr.io/mikeo/harborbuddy:latest`

- **For main branch commits**:
  - `ghcr.io/mikeo/harborbuddy:main`
  - `ghcr.io/mikeo/harborbuddy:main-abc1234` (SHA)

- **For pull requests** (not pushed):
  - `ghcr.io/mikeo/harborbuddy:pr-123`

### 3. Release Workflow (`.github/workflows/release.yml`)

**Triggers**: Git tags starting with `v*`

**Features**:
- Multi-architecture builds
- Semantic versioning
- Push to GitHub Container Registry (GHCR)

## Testing the CI/CD Pipeline

### Step 1: Test on a Branch (Safe)

```bash
# Create a test branch
git checkout -b test-cicd

# Make a small change (e.g., update version in README)
echo "Testing CI/CD" >> README.md

# Commit and push
git add README.md
git commit -m "test: Validate CI/CD pipeline"
git push origin test-cicd

# Create a Pull Request on GitHub
# Check the Actions tab to see:
# - Tests running
# - Docker image building (but not pushing)
```

### Step 2: Test Main Branch Build

```bash
# After PR is merged to main
# Check GitHub Actions tab
# You should see:
# 1. CI workflow running tests
# 2. Docker Build workflow creating and pushing images
# 3. Images tagged with 'main' and 'main-<sha>'
```

### Step 3: Create a Release (Full Test)

```bash
# Make sure you're on main and up to date
git checkout main
git pull origin main

# Create a version tag
git tag -a v0.1.1 -m "Release v0.1.1"

# Push the tag
git push origin v0.1.1

# Monitor GitHub Actions
# You should see:
# 1. Docker Build workflow triggered
# 2. Multi-arch images being built
# 3. Images pushed with multiple tags
```

### Step 4: Verify the Images

```bash
# Check available tags on GHCR
# Visit: https://github.com/mikeo/harborbuddy/pkgs/container/harborbuddy

# Or use GitHub CLI
gh api /users/mikeo/packages/container/harborbuddy/versions

# Pull and test the image (amd64)
docker pull ghcr.io/mikeo/harborbuddy:latest
docker run --rm ghcr.io/mikeo/harborbuddy:latest --version

# Check the image architecture
docker image inspect ghcr.io/mikeo/harborbuddy:latest | grep Architecture

# Pull specific architecture (if on different platform)
docker pull --platform linux/arm64 ghcr.io/mikeo/harborbuddy:latest
docker pull --platform linux/amd64 ghcr.io/mikeo/harborbuddy:latest
```

## Quick Test Procedure

Here's a simple test to validate everything:

### 1. Make a Test Commit

```bash
# Edit a non-critical file
cat >> README.md << 'EOF'

<!-- CI/CD Test -->
EOF

git add README.md
git commit -m "docs: Test CI/CD pipeline"
git push origin main
```

### 2. Watch GitHub Actions

Go to: `https://github.com/mikeo/harborbuddy/actions`

You should see:
- ✅ CI workflow running and passing
- ✅ Docker Build workflow building multi-arch images
- ✅ Images pushed to GHCR

### 3. Create a Test Release

```bash
# Tag the commit
git tag v0.1.1
git push origin v0.1.1
```

Watch Actions again - you should see the release workflow create:
- `ghcr.io/mikeo/harborbuddy:0.1.1`
- `ghcr.io/mikeo/harborbuddy:0.1`
- `ghcr.io/mikeo/harborbuddy:0`
- `ghcr.io/mikeo/harborbuddy:latest`

All with 3 architectures each!

### 4. Verify Multi-Arch

```bash
# Use docker manifest to check architectures
docker manifest inspect ghcr.io/mikeo/harborbuddy:latest | grep -A3 platform

# Expected output:
#   "platform": {
#     "architecture": "amd64",
#     "os": "linux"
#   --
#   "platform": {
#     "architecture": "arm64",
#     "os": "linux"
#   --
#   "platform": {
#     "architecture": "arm",
#     "os": "linux",
#     "variant": "v7"
```

## Image Registry

HarborBuddy images are hosted on **GitHub Container Registry (GHCR)**:

- Registry: `ghcr.io`
- Repository: `ghcr.io/mikeo/harborbuddy`
- Visibility: Public (can be pulled without authentication)

## Authentication (for pushing)

GitHub Actions automatically authenticates using `GITHUB_TOKEN`. No additional secrets needed!

For manual pushes:
```bash
# Create a Personal Access Token (PAT) with write:packages permission
echo $GITHUB_TOKEN | docker login ghcr.io -u USERNAME --password-stdin
```

## Versioning Strategy

### Semantic Versioning
- Major version (breaking changes): `v1.0.0` → `v2.0.0`
- Minor version (new features): `v1.0.0` → `v1.1.0`
- Patch version (bug fixes): `v1.0.0` → `v1.0.1`

### Creating Releases

```bash
# For a new feature
git tag -a v1.1.0 -m "feat: Add new feature"
git push origin v1.1.0

# For a bug fix
git tag -a v1.0.1 -m "fix: Fix critical bug"
git push origin v1.0.1

# For breaking changes
git tag -a v2.0.0 -m "BREAKING CHANGE: New API"
git push origin v2.0.0
```

## Troubleshooting

### Build Fails

Check the Actions tab for error messages:
```bash
# Common issues:
# 1. Tests failing - Fix the code
# 2. Format issues - Run: go fmt ./...
# 3. Linter errors - Run: go vet ./...
```

### Image Not Pushed

Check:
1. Is it a PR? PRs only build, don't push
2. Do you have permissions? Check repo settings
3. Is GITHUB_TOKEN working? It should be automatic

### Wrong Architecture

Pull specific architecture:
```bash
docker pull --platform linux/amd64 ghcr.io/mikeo/harborbuddy:latest
docker pull --platform linux/arm64 ghcr.io/mikeo/harborbuddy:latest
```

### Can't Find Image

Check the package visibility:
1. Go to repository settings
2. Navigate to Packages
3. Ensure harborbuddy package is set to Public

## Build Performance

### Build Times (Approximate)
- Single architecture: ~2-3 minutes
- Multi-architecture (3): ~5-7 minutes
- With cache: ~1-2 minutes

### Caching
The workflow uses GitHub Actions cache:
- `cache-from: type=gha` - Read from cache
- `cache-to: type=gha,mode=max` - Write to cache
- Speeds up subsequent builds by ~50%

## Monitoring

### GitHub Actions Dashboard
View all workflows: `https://github.com/mikeo/harborbuddy/actions`

### Package Dashboard
View all images: `https://github.com/mikeo/harborbuddy/pkgs/container/harborbuddy`

### Build Logs
Click on any workflow run to see detailed logs for:
- Each build stage
- Test results
- Image push status

## Best Practices

### Before Pushing to Main
1. ✅ Run tests locally: `go test ./...`
2. ✅ Format code: `go fmt ./...`
3. ✅ Build locally: `go build ./cmd/harborbuddy`
4. ✅ Test Docker build: `docker build -t test .`

### For Releases
1. ✅ Update CHANGELOG.md
2. ✅ Update version in code (if hardcoded)
3. ✅ Merge all changes to main
4. ✅ Create and push tag
5. ✅ Verify build in Actions
6. ✅ Test the released image

### Tag Naming
- Use semantic versioning: `v1.2.3`
- Always start with `v`
- Include release notes in tag message

## Example End-to-End Flow

```bash
# 1. Develop feature
git checkout -b feature/new-feature
# ... make changes ...
git commit -m "feat: Add awesome feature"

# 2. Push and create PR
git push origin feature/new-feature
# Create PR on GitHub, wait for CI to pass

# 3. Merge to main
# PR gets merged, Docker image built and pushed with 'main' tag

# 4. Create release
git checkout main
git pull origin main
git tag -a v0.2.0 -m "Release v0.2.0: Add awesome feature"
git push origin v0.2.0

# 5. Verify release
# Check GitHub Actions for successful build
# Check GHCR for new tags: 0.2.0, 0.2, 0, latest

# 6. Test the image
docker pull ghcr.io/mikeo/harborbuddy:0.2.0
docker run --rm ghcr.io/mikeo/harborbuddy:0.2.0 --version
# Should output: HarborBuddy version 0.2.0
```

## Summary

✅ **Multi-arch builds**: amd64, arm64, arm/v7  
✅ **Automatic versioning**: From git tags  
✅ **Auto-deploy**: On tag push  
✅ **Image caching**: Fast subsequent builds  
✅ **Multiple tags**: Semantic versions + latest  
✅ **Test integration**: CI validates before release  

Your CI/CD is ready to go! Just push code and tags. 🚀

