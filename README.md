# Foundry Artifact Registry

Foundry is a self-hosted artifact registry for versioned binary files. It provides:

- Content-addressed blob storage (`sha256`) on disk
- SQLite metadata for package/version lookup
- Token-based API authentication
- HTTP API for upload, download, list, search, delete, and garbage collection
- Streaming upload/download paths for large files
- CLI client for push/pull/list/search/delete
- Structured JSON request logging with request IDs

## Project Layout

```text
cmd/
  registry-server/
  registry-cli/
internal/
  core/
    models/
    services/
  adapters/
    storage/
    metadata/
    auth/
  api/
    handlers/
  util/
    hashing/
    logging/
```

## Build

```bash
go build -o registry-server ./cmd/registry-server
go build -o registry-cli ./cmd/registry-cli
```

## Configuration

Default config file: `config.yaml`

```yaml
server:
  port: 8080
storage:
  dataDir: ./data
auth:
  tokens:
    - "dev-token"
    - "prod-token"
```

Run server:

```bash
./registry-server -config ./config.yaml
```


## Testing

```bash
go test ./...
```
