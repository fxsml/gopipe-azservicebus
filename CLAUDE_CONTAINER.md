# Claude Code Container - Local Development Environment

This setup provides an isolated Docker container with Claude Code CLI for "yolo deployment" - a local development environment that mimics cloud coding but runs entirely on your machine.

## Quick Start

### 1. Start the Container
```bash
make claude-start
```

This will:
- Build the Docker image (if needed)
- Start the container in detached mode
- Clone the repository to `/workspace/repo`
- Keep the container running persistently

### 2. Attach to the Container
```bash
make claude-attach
```

Opens an interactive shell in the `/workspace/repo` directory.

### 3. Set Up Claude Code (First Time Only)

Inside the container, set your API key and login:

```bash
# Set the API key
export ANTHROPIC_API_KEY="your-api-key-here"

# Login to Claude Code
claude login

# Start using Claude Code
claude
```

Alternatively, set the API key when starting the container:

```bash
ANTHROPIC_API_KEY="your-key" make claude-start
```

## Available Commands

| Command | Description |
|---------|-------------|
| `make claude-start` | Start/create the container (idempotent) |
| `make claude-stop` | Stop the container (preserves state) |
| `make claude-restart` | Restart the container |
| `make claude-attach` | Attach to workspace shell (`/workspace/repo`) |
| `make claude-shell` | Open shell at container root |
| `make claude-status` | Show container status |
| `make claude-logs` | View container logs |
| `make claude-remove` | Completely remove the container |
| `make claude-build` | Rebuild the Docker image |

## Container Features

### Installed Tools
- **Go 1.21.5**: Full Go development environment
- **Node.js 20**: For Claude Code CLI and JavaScript tools
- **Claude Code CLI 2.0.50**: Latest Claude Code interface
- **golangci-lint**: Go linting tool
- **Git**: Version control with sensible defaults
- **Development tools**: vim, nano, jq, wget, curl

### Container Configuration
- **Persistent**: Container stays running and retains state between sessions
- **Docker-in-Docker**: Access to host Docker socket for container operations
- **Workspace**: Repository cloned to `/workspace/repo`
- **Git Config**: Pre-configured with default user (can be changed)

## Workflow Examples

### Basic Development Session
```bash
# Start the container
make claude-start

# Attach to workspace
make claude-attach

# Inside container - run Claude Code
export ANTHROPIC_API_KEY="your-key"
claude

# Make changes, commit, etc.
git status
go test ./...
```

### Check Container Status
```bash
make claude-status
```

### Stop for the Day (Preserves State)
```bash
make claude-stop
```

### Resume Next Day
```bash
make claude-start  # Restarts existing container
make claude-attach
```

### Clean Slate
```bash
make claude-remove  # Remove everything
make claude-start   # Create fresh container
```

## Persistence

The container is designed to be **persistent**:
- Stopping the container preserves all changes inside it
- Code changes, installed packages, and configurations remain intact
- Only `make claude-remove` deletes everything

## Environment Variables

### ANTHROPIC_API_KEY
Set at container creation or inside the container:

```bash
# Option 1: Set when starting
ANTHROPIC_API_KEY="your-key" make claude-start

# Option 2: Set inside container (persistent within container)
docker exec gopipe-azservicebus-claude bash -c 'echo "export ANTHROPIC_API_KEY=your-key" >> ~/.bashrc'

# Option 3: Set temporarily in session
make claude-attach
export ANTHROPIC_API_KEY="your-key"
```

## Troubleshooting

### Container Won't Start
```bash
# Check status
make claude-status

# View logs
make claude-logs

# Clean restart
make claude-remove
make claude-start
```

### Container Exists But Won't Attach
```bash
# The attach command will auto-start if stopped
make claude-attach

# Or manually restart
make claude-restart
```

### Reset Everything
```bash
make claude-remove
make claude-build
make claude-start
```

## Architecture

```
Host Machine
├── Makefile (orchestration)
├── Dockerfile.claude (container definition)
└── Docker Container
    ├── /workspace/repo (your code)
    ├── Claude Code CLI
    ├── Go toolchain
    └── Development tools
```

## Security Notes

1. **Docker Socket**: Container has access to host Docker socket (`/var/run/docker.sock`)
   - Required for some development workflows
   - Container is isolated but can interact with Docker

2. **API Key**: Best practices:
   - Don't commit API keys to git
   - Use environment variables
   - Consider using a `.env` file (add to `.gitignore`)

3. **Container Isolation**: The container provides isolation from your host but:
   - Has network access
   - Can access Docker on host
   - Runs as root inside container

## Best Practices

1. **Start/Stop vs Remove**:
   - Use `claude-stop`/`claude-start` for daily work
   - Use `claude-remove` only when you want a clean slate

2. **API Key Management**:
   - Set once inside container and it persists
   - Or use environment variable for automation

3. **Version Control**:
   - Commit changes from inside the container
   - Or mount your local repo for live editing

4. **Updates**:
   - Rebuild image for tool updates: `make claude-build`
   - Remove and recreate for clean environment

## Tips

- Use `make claude-shell` for general container access
- Use `make claude-attach` to jump directly to your code
- Container stays running - no need to restart frequently
- Check `make help` for all available commands
