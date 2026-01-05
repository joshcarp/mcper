#!/bin/bash
# MCPER Chrome Extension Installer
# Downloads and sets up the Chrome extension and native bridge

set -e

VERSION="${1:-latest}"
INSTALL_DIR="${HOME}/.mcper/chrome-extension"
REPO="joshcarp/mcper"

echo "=== MCPER Chrome Extension Installer ==="
echo ""

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "Detected: $OS-$ARCH"
echo "Installing to: $INSTALL_DIR"
echo ""

# Create install directory
mkdir -p "$INSTALL_DIR"

# Get version
if [ "$VERSION" = "latest" ]; then
    VERSION=$(curl -sL "https://storage.googleapis.com/mcper-releases/latest.json" | grep -o '"version": "[^"]*"' | cut -d'"' -f4)
    echo "Latest version: v$VERSION"
fi

echo ""
echo "Downloading extension files from GitHub..."

# Download extension files
GITHUB_RAW="https://raw.githubusercontent.com/$REPO/main/extension/chrome"

curl -sL "$GITHUB_RAW/manifest.json" -o "$INSTALL_DIR/manifest.json"
curl -sL "$GITHUB_RAW/background.js" -o "$INSTALL_DIR/background.js"
curl -sL "$GITHUB_RAW/content.js" -o "$INSTALL_DIR/content.js"
curl -sL "$GITHUB_RAW/popup.html" -o "$INSTALL_DIR/popup.html"
curl -sL "$GITHUB_RAW/popup.js" -o "$INSTALL_DIR/popup.js"

# Download icons
mkdir -p "$INSTALL_DIR/icons"
curl -sL "$GITHUB_RAW/icons/icon16.png" -o "$INSTALL_DIR/icons/icon16.png"
curl -sL "$GITHUB_RAW/icons/icon48.png" -o "$INSTALL_DIR/icons/icon48.png"
curl -sL "$GITHUB_RAW/icons/icon128.png" -o "$INSTALL_DIR/icons/icon128.png"

echo "Extension files downloaded."
echo ""

# Download chrome-bridge binary
echo "Downloading chrome-bridge binary..."
BRIDGE_URL="https://storage.googleapis.com/mcper-releases/v$VERSION/chrome-bridge-$OS-$ARCH"
BRIDGE_PATH="$INSTALL_DIR/chrome-bridge"

# Try versioned path first, fall back to building from source
if curl -sL --fail "$BRIDGE_URL" -o "$BRIDGE_PATH" 2>/dev/null; then
    chmod +x "$BRIDGE_PATH"
    echo "chrome-bridge downloaded."
else
    echo "chrome-bridge binary not available for your platform."
    echo "You can build it from source:"
    echo "  git clone https://github.com/$REPO.git"
    echo "  cd mcper && go build -o chrome-bridge ./cmd/chrome-bridge/"
    echo ""
    # Create a placeholder script
    cat > "$BRIDGE_PATH" << 'SCRIPT'
#!/bin/bash
echo "chrome-bridge not installed. Build from source:"
echo "  go build -o ~/.mcper/chrome-extension/chrome-bridge ./cmd/chrome-bridge/"
exit 1
SCRIPT
    chmod +x "$BRIDGE_PATH"
fi

echo ""
echo "=== Installation Complete ==="
echo ""
echo "Extension installed to: $INSTALL_DIR"
echo ""
echo "=== Next Steps ==="
echo ""
echo "1. Load the extension in Chrome:"
echo "   - Open chrome://extensions/"
echo "   - Enable 'Developer mode' (top right)"
echo "   - Click 'Load unpacked'"
echo "   - Select: $INSTALL_DIR"
echo "   - Note the Extension ID"
echo ""
echo "2. Start the HTTP bridge:"
echo "   $BRIDGE_PATH -server -port 9223"
echo ""
echo "3. Test the connection:"
echo "   curl http://localhost:9223/health"
echo ""
echo "4. (Optional) Set up native messaging for better integration:"
echo "   See: https://github.com/$REPO/blob/main/extension/chrome/README.md"
echo ""
