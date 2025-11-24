# Claude Code Development Container

This Docker container provides an isolated development environment for working with the gopipe-azservicebus project.

## Features

- Ubuntu 22.04 base
- Go 1.21.5
- Node.js 20.x
- Git, build tools, and development utilities
- golangci-lint for code quality
- Docker socket access for running containers

## Usage

### Build the image

```bash
make claude-build
```

### Start the container

```bash
make claude-yolo ANTHROPIC_API_KEY=your-api-key-here
```

Or export the key first:
```bash
export ANTHROPIC_API_KEY=sk-ant-your-key-here
make claude-yolo
```

### Access the container

```bash
# Open bash shell in the repository directory
make claude-attach

# Or open bash shell at workspace root
make claude-shell
```

### Stop and remove the container

```bash
make claude-stop
```

## Inside the Container

Once attached, you can:

1. **Work with the code**
   ```bash
   cd /workspace/repo
   go test ./...
   go build ./...
   ```

2. **Use development tools**
   ```bash
   make fmt
   make vet
   make lint
   ```

3. **Run tests**
   ```bash
   make test-unit
   ```

## Notes

- The container clones the `main` branch from GitHub
- The Docker socket is mounted for running Docker commands inside the container
- The `ANTHROPIC_API_KEY` environment variable is available for API usage
- All standard Go and Node.js tools are available
