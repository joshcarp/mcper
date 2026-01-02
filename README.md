# mcper

Zero-config MCP tool aggregation with WASM plugins.

mcper lets you add MCP (Model Context Protocol) tools to any project with a single command. Plugins run in WASM sandboxes for security.

## Quick Start

```bash
# Install mcper
curl -sSL https://raw.githubusercontent.com/joshcarp/mcper/main/scripts/release/install.sh | sh

# Initialize in your project
mcper init

# Add plugins
mcper add github
mcper add gmail

# Enable for Claude Code
mcper enable --claude
```

## How It Works

1. `mcper init` creates `.mcper/start.sh` - a self-bootstrapping script that auto-installs mcper
2. `mcper add <plugin>` adds plugins to your configuration
3. Configure your MCP client to run `.mcper/start.sh`
4. mcper aggregates all plugins into a single MCP server

Once `.mcper/start.sh` is committed, anyone who clones the repo will have mcper auto-download and start - no setup required.

## Available Plugins

| Plugin | Description | Required Env Vars |
|--------|-------------|-------------------|
| github | GitHub API - repos, issues, PRs | `GITHUB_TOKEN` |
| gmail | Gmail API - read, send emails | `GMAIL_ACCESS_TOKEN` |
| linkedin | LinkedIn API - profiles, messages | `LINKEDIN_CLIENT_ID`, `LINKEDIN_CLIENT_SECRET` |
| azuredevops | Azure DevOps - projects, repos, pipelines | `AZURE_DEVOPS_PAT`, `AZURE_DEVOPS_ORG` |

```bash
# List all available plugins
mcper list
```

## Commands

```bash
mcper init              # Initialize .mcper/start.sh
mcper add <plugin>      # Add a plugin
mcper list              # List available plugins
mcper enable --claude   # Add to .mcp.json for Claude Code
mcper serve             # Run MCP server (called by start.sh)
mcper update            # Update mcper to latest version
mcper cache list        # List cached plugins
mcper cache clean       # Clear plugin cache
```

## Configuration

After adding plugins, your `.mcp.json` will look like:

```json
{
  "mcpServers": {
    "my-project": {
      "command": "./.mcper/start.sh",
      "env": {
        "GITHUB_TOKEN": "${GITHUB_TOKEN}"
      }
    }
  }
}
```

## Building from Source

```bash
# Build CLI
make build

# Build WASM plugins
make wasm-build

# Run tests
make test
```

## License

Apache License 2.0 - see [LICENSE](LICENSE)
