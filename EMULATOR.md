# Azure Service Bus Emulator Setup

This guide explains how to run the Azure Service Bus emulator locally on macOS (both Intel and Apple Silicon).

## Prerequisites

- Docker Desktop for Mac (with Rosetta 2 support for Apple Silicon)
- Go 1.24 or later

## Quick Start

### 1. Start the Emulator

```bash
make emulator-start
```

This will:
- Start the Azure Service Bus emulator on port 5678
- Start SQL Edge as the backing store on port 1433
- Configure queues and topics from `config/config.json`

### 2. Verify the Emulator is Running

```bash
make emulator-status
```

You should see both containers running:
- `servicebus-emulator` - The Service Bus emulator
- `servicebus-sql` - SQL Edge backing store

### 3. Run the Integration Tests

```bash
make test-integration
```

This will run tests that:
- Connect to the emulator
- Publish messages to a queue
- Receive and verify messages

### 4. Stop the Emulator

```bash
make emulator-stop
```

## Manual Docker Commands

If you prefer not to use the Makefile:

```bash
# Start
docker compose up -d

# View logs
docker compose logs -f

# Stop
docker compose down

# Stop and remove volumes
docker compose down -v
```

## Connection String

The emulator uses the following connection string:

```
Endpoint=sb://localhost;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true;
```

You can override this by setting the `SERVICEBUS_CONNECTION_STRING` environment variable.

## Configuration

Queues and topics are configured in `config/config.json`. The emulator will create these entities on startup.

### Available Test Queues

- `test-queue-001` - Basic test queue
- `test-queue-002` - Basic test queue
- `test-queue-batch` - For batch testing
- `test-queue-concurrent` - For concurrency testing
- `test-queue-deadletter` - For dead-letter testing

### Available Test Topics

- `test-topic-001` with subscription `test-sub-001`
- `test-topic-multi` with subscriptions `sub-001` and `sub-002`

## Known Issues

### DNS Resolution Issues on macOS

There is a known issue with DNS resolution between the Service Bus emulator container (.NET/x64) and the SQL Edge container on macOS, particularly with Apple Silicon. The emulator may show as "unhealthy" in Docker status even when it's actually functional.

**Workaround**: The emulator will eventually connect after several retries (15-30 seconds between retries). You can:

1. Wait 2-3 minutes for the emulator to fully initialize
2. Check logs: `docker compose logs servicebus | grep "Emulator is ready"`
3. Test connectivity directly: `curl http://localhost:5678`
4. Run tests anyway - the tests may work even if healthcheck shows unhealthy

If the emulator doesn't start after 5 minutes, try:
```bash
docker compose down -v  # Remove volumes
docker compose up -d
```

## Troubleshooting

### Emulator won't start on Apple Silicon

Make sure you have:
1. Docker Desktop with Rosetta 2 support enabled
2. The latest version of Docker Desktop

You can enable Rosetta 2 in Docker Desktop:
- Go to Settings → General → "Use Rosetta for x86_64/amd64 emulation on Apple Silicon"

### SQL container fails health check

The SQL container may take 30-60 seconds to fully initialize. Check the logs:

```bash
docker compose logs sql
```

### Port conflicts

If ports 5678 or 1433 are already in use, you can modify them in `compose.yaml`.

### Connection timeouts

If tests fail with connection timeouts:
1. Verify the emulator is running: `docker compose ps`
2. Check the logs: `docker compose logs servicebus`
3. Wait a bit longer - the emulator may still be initializing

## Platform Notes

### Apple Silicon (M1/M2/M3)

The setup uses:
- `linux/arm64` for SQL Edge (native ARM performance)
- `linux/amd64` for Service Bus emulator (runs via Rosetta 2)

This combination provides the best performance and compatibility.

### Intel Macs

Both containers will run natively as `linux/amd64`.

## Running Tests

### All tests
```bash
make test
```

### Integration tests only
```bash
make test-integration
```

### Unit tests only (skip integration tests)
```bash
make test-unit
```

### With coverage
```bash
make test-coverage
```

## Development Workflow

1. Start the emulator: `make emulator-start`
2. Make your changes
3. Run tests: `make test`
4. View logs if needed: `make emulator-logs`
5. Stop when done: `make emulator-stop`

## Clean Up

To completely remove all emulator data:

```bash
make clean
```

This will:
- Stop all containers
- Remove volumes (clearing all data)
- Remove test artifacts
