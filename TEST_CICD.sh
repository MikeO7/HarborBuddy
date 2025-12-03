#!/bin/bash
set -e

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  HarborBuddy CI/CD Test Script${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Function to print status
status() {
    echo -e "${GREEN}✓${NC} $1"
}

warn() {
    echo -e "${YELLOW}⚠${NC}  $1"
}

error() {
    echo -e "${RED}✗${NC} $1"
}

info() {
    echo -e "${BLUE}ℹ${NC}  $1"
}

# Check if we're in the right directory
if [ ! -f "go.mod" ] || [ ! -d ".github" ]; then
    error "Not in HarborBuddy directory"
    exit 1
fi

status "In HarborBuddy directory"

# Check Git status
if ! git status &>/dev/null; then
    error "Not a git repository"
    exit 1
fi

status "Git repository detected"

# Check current branch
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
info "Current branch: $CURRENT_BRANCH"

# Check for uncommitted changes
if ! git diff-index --quiet HEAD --; then
    warn "You have uncommitted changes"
    git status --short
    echo ""
    read -p "Continue anyway? (y/n) " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

status "Repository is clean or continuing with changes"

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Test Options${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo "1. Make a test commit to current branch"
echo "2. Create and push a test tag"
echo "3. Check GitHub Actions status"
echo "4. Verify Docker images"
echo "5. Full end-to-end test"
echo ""
read -p "Select option (1-5): " -n 1 -r OPTION
echo ""
echo ""

case $OPTION in
    1)
        echo -e "${BLUE}Making test commit...${NC}"
        echo ""
        
        # Add a comment to README
        echo "<!-- CI/CD test $(date +%s) -->" >> README.md
        git add README.md
        git commit -m "test: CI/CD pipeline validation"
        
        status "Test commit created"
        info "Commit: $(git log -1 --oneline)"
        echo ""
        
        read -p "Push to origin/$CURRENT_BRANCH? (y/n) " -n 1 -r
        echo ""
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            git push origin $CURRENT_BRANCH
            status "Pushed to origin/$CURRENT_BRANCH"
            echo ""
            info "Check GitHub Actions: https://github.com/mikeo/harborbuddy/actions"
        fi
        ;;
        
    2)
        echo -e "${BLUE}Creating test tag...${NC}"
        echo ""
        
        # Get current tags
        echo "Recent tags:"
        git tag -l --sort=-version:refname | head -5
        echo ""
        
        read -p "Enter new tag (e.g., v0.1.1): " TAG
        if [ -z "$TAG" ]; then
            error "Tag cannot be empty"
            exit 1
        fi
        
        # Check if tag exists
        if git rev-parse "$TAG" >/dev/null 2>&1; then
            error "Tag $TAG already exists"
            exit 1
        fi
        
        read -p "Enter tag message: " MSG
        if [ -z "$MSG" ]; then
            MSG="Release $TAG"
        fi
        
        git tag -a "$TAG" -m "$MSG"
        status "Tag $TAG created"
        echo ""
        
        read -p "Push tag to origin? (y/n) " -n 1 -r
        echo ""
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            git push origin "$TAG"
            status "Tag pushed to origin"
            echo ""
            info "Check GitHub Actions: https://github.com/mikeo/harborbuddy/actions"
            info "Check GHCR: https://github.com/mikeo/harborbuddy/pkgs/container/harborbuddy"
        fi
        ;;
        
    3)
        echo -e "${BLUE}Checking GitHub Actions...${NC}"
        echo ""
        
        if ! command -v gh &> /dev/null; then
            warn "GitHub CLI (gh) not installed"
            info "Install: brew install gh"
            info "Or visit: https://github.com/mikeo/harborbuddy/actions"
        else
            echo "Recent workflow runs:"
            gh run list --limit 5
            echo ""
            status "Showing recent runs"
        fi
        ;;
        
    4)
        echo -e "${BLUE}Verifying Docker images...${NC}"
        echo ""
        
        if ! command -v docker &> /dev/null; then
            error "Docker not installed"
            exit 1
        fi
        
        IMAGE="ghcr.io/mikeo/harborbuddy:latest"
        info "Pulling $IMAGE"
        
        if docker pull $IMAGE 2>&1; then
            status "Image pulled successfully"
            echo ""
            
            info "Image details:"
            docker image inspect $IMAGE | grep -E "Architecture|Os" | head -2
            echo ""
            
            info "Testing version:"
            docker run --rm $IMAGE --version
            echo ""
            
            info "Checking manifest for multi-arch:"
            docker manifest inspect $IMAGE | grep -A3 '"platform"' | head -12
            
            status "Image verification complete"
        else
            error "Failed to pull image"
            warn "Image might not exist yet or repo might be private"
            info "Check: https://github.com/mikeo/harborbuddy/pkgs/container/harborbuddy"
        fi
        ;;
        
    5)
        echo -e "${BLUE}Running full end-to-end test...${NC}"
        echo ""
        
        # Step 1: Test commit
        echo -e "${YELLOW}Step 1/4: Creating test commit${NC}"
        echo "<!-- CI/CD E2E test $(date +%s) -->" >> README.md
        git add README.md
        git commit -m "test: End-to-end CI/CD validation"
        status "Test commit created"
        echo ""
        
        # Step 2: Push
        echo -e "${YELLOW}Step 2/4: Pushing commit${NC}"
        git push origin $CURRENT_BRANCH
        status "Pushed to $CURRENT_BRANCH"
        echo ""
        
        # Step 3: Wait for CI
        echo -e "${YELLOW}Step 3/4: Waiting for CI (30 seconds)${NC}"
        for i in {30..1}; do
            echo -ne "\rWaiting... $i seconds "
            sleep 1
        done
        echo ""
        status "Wait complete"
        echo ""
        
        # Step 4: Check status
        echo -e "${YELLOW}Step 4/4: Checking status${NC}"
        if command -v gh &> /dev/null; then
            gh run list --limit 3
            echo ""
            status "Recent workflow runs shown above"
        else
            info "Visit: https://github.com/mikeo/harborbuddy/actions"
        fi
        
        echo ""
        status "End-to-end test complete"
        echo ""
        info "Next steps:"
        echo "  1. Check GitHub Actions for build status"
        echo "  2. Verify images at GHCR"
        echo "  3. Create a release tag: git tag v0.1.1 && git push origin v0.1.1"
        ;;
        
    *)
        error "Invalid option"
        exit 1
        ;;
esac

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Test Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "Useful commands:"
echo "  • View Actions: gh run list"
echo "  • View logs:    gh run view"
echo "  • Check images: docker manifest inspect ghcr.io/mikeo/harborbuddy:latest"
echo "  • Pull image:   docker pull ghcr.io/mikeo/harborbuddy:latest"
echo ""

