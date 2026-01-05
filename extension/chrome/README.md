# MCPER Chrome Bridge Extension

Browser automation extension for MCPER - enables AI agents to control Chrome tabs via MCP tools.

## Architecture

```
┌─────────────────┐    MCP/stdio    ┌─────────────┐    HTTP     ┌──────────────────┐
│  MCPER Daemon   │◄───────────────►│ WASM Plugin │◄───────────►│ Chrome Bridge    │
│                 │                 │ (chrome)    │  :9223      │ (native host)    │
│ wasm/chrome/*   │                 │             │             │                  │
└─────────────────┘                 └─────────────┘             │    Native Msg    │
                                                                │        ▼         │
                                                                │ Chrome Extension │
                                                                │ (controls tabs)  │
                                                                └──────────────────┘
```

## Components

1. **Chrome Extension** - Runs in browser, controls tabs via Chrome APIs
2. **Native Messaging Host** (`chrome-bridge`) - HTTP server bridging WASM plugin to extension
3. **WASM Plugin** - MCP server exposing browser control tools

## Installation

### 1. Build the Native Host

```bash
cd /path/to/mcper-public
make chrome-bridge
```

### 2. Install the Native Messaging Host

**macOS:**
```bash
# Copy binary
sudo cp bin/chrome-bridge /usr/local/bin/

# Create manifest directory
mkdir -p ~/Library/Application\ Support/Google/Chrome/NativeMessagingHosts/

# Copy manifest (update EXTENSION_ID first!)
cp extension/chrome/native-messaging-host.json \
   ~/Library/Application\ Support/Google/Chrome/NativeMessagingHosts/com.mcper.chrome_bridge.json
```

**Linux:**
```bash
# Copy binary
sudo cp bin/chrome-bridge /usr/local/bin/

# Create manifest directory
mkdir -p ~/.config/google-chrome/NativeMessagingHosts/

# Copy manifest (update EXTENSION_ID first!)
cp extension/chrome/native-messaging-host.json \
   ~/.config/google-chrome/NativeMessagingHosts/com.mcper.chrome_bridge.json
```

**Windows:**
```powershell
# Copy binary to PATH
copy bin\chrome-bridge.exe C:\Program Files\MCPER\

# Create registry key (run as admin)
# HKEY_CURRENT_USER\Software\Google\Chrome\NativeMessagingHosts\com.mcper.chrome_bridge
# Set default value to path of manifest JSON
```

### 3. Load the Chrome Extension

1. Open Chrome and go to `chrome://extensions/`
2. Enable "Developer mode" (toggle in top right)
3. Click "Load unpacked"
4. Select the `extension/chrome` directory
5. Note the Extension ID (you'll need it for the manifest)

### 4. Update the Native Messaging Manifest

Edit the native messaging host manifest and replace `EXTENSION_ID_HERE` with your extension's ID:

```json
{
  "allowed_origins": [
    "chrome-extension://YOUR_EXTENSION_ID_HERE/"
  ]
}
```

### 5. Start the HTTP Bridge Server

```bash
chrome-bridge -server -port 9223
```

Or run as a background service.

### 6. Build the WASM Plugin

```bash
make wasm-build
```

### 7. Add to MCPER

```bash
mcper add chrome
```

## Available MCP Tools

### Page Automation
| Tool | Description |
|------|-------------|
| `chrome_navigate` | Navigate to a URL |
| `chrome_click` | Click an element by CSS selector |
| `chrome_type` | Type text into an input element |
| `chrome_scroll` | Scroll page or element into view |
| `chrome_wait` | Wait for element to appear |

### Content Extraction
| Tool | Description |
|------|-------------|
| `chrome_screenshot` | Capture tab screenshot (base64) |
| `chrome_get_html` | Get page/element HTML |
| `chrome_get_text` | Get page/element text content |
| `chrome_evaluate` | Execute JavaScript |

### Tab Management
| Tool | Description |
|------|-------------|
| `chrome_list_tabs` | List all open tabs |
| `chrome_switch_tab` | Switch to tab by ID |
| `chrome_new_tab` | Open new tab |
| `chrome_close_tab` | Close tab |

### Advanced
| Tool | Description |
|------|-------------|
| `chrome_cdp` | Send raw CDP command |

## Example Usage

Once installed, Claude can use commands like:

```
Navigate to github.com, find the search input,
type "mcper", and take a screenshot of the results.
```

This translates to:
1. `chrome_navigate` with url="https://github.com"
2. `chrome_type` with selector="input[name='q']", text="mcper"
3. `chrome_click` with selector="button[type='submit']"
4. `chrome_wait` with selector=".repo-list"
5. `chrome_screenshot`

## Troubleshooting

### Extension not connecting to native host

1. Check the native messaging manifest path is correct
2. Verify the extension ID in `allowed_origins`
3. Check Chrome's native messaging logs: `chrome://extensions/` → Details → "Inspect views"

### HTTP server not responding

1. Ensure `chrome-bridge -server` is running
2. Check port 9223 is not in use: `lsof -i :9223`
3. Test with: `curl http://localhost:9223/health`

### WASM plugin can't connect

1. Verify `MCPER_CHROME_BRIDGE_URL` env var (default: `http://localhost:9223`)
2. Check daemon logs for connection errors

## Security Notes

- The extension requires broad permissions to control any tab
- Native messaging host only accepts connections from the specific extension ID
- HTTP bridge binds to localhost only
- Consider firewall rules in production

## Development

### Generate PNG icons from SVG

```bash
# Requires ImageMagick or rsvg-convert
for size in 16 48 128; do
  convert -background none icons/icon${size}.svg icons/icon${size}.png
done
```

### Testing commands manually

```bash
# Test via HTTP
curl -X POST http://localhost:9223/command \
  -H "Content-Type: application/json" \
  -d '{"command": "list_tabs"}'

# Test navigation
curl -X POST http://localhost:9223/navigate \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com"}'
```
