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

## API (v1)

All endpoints require:

```text
Authorization: Bearer <token>
```

Routes:

- `POST   /api/v1/artifacts/{package}/{version}`
- `GET    /api/v1/artifacts/{package}/{version}`
- `GET    /api/v1/packages`
- `GET    /api/v1/packages/{package}`
- `DELETE /api/v1/artifacts/{package}/{version}`
- `POST   /api/v1/gc`

## cURL Examples

Upload:

```bash
curl -X POST \
  -H "Authorization: Bearer dev-token" \
  -H "Content-Type: application/octet-stream" \
  --data-binary @./file.tar.gz \
  http://localhost:8080/api/v1/artifacts/mypkg/1.0.0
```

Download:

```bash
curl -L \
  -H "Authorization: Bearer dev-token" \
  -o ./file.tar.gz \
  http://localhost:8080/api/v1/artifacts/mypkg/1.0.0
```

List packages:

```bash
curl -H "Authorization: Bearer dev-token" \
  http://localhost:8080/api/v1/packages
```

Search packages:

```bash
curl -H "Authorization: Bearer dev-token" \
  "http://localhost:8080/api/v1/packages?search=mypkg"
```

Get package versions:

```bash
curl -H "Authorization: Bearer dev-token" \
  http://localhost:8080/api/v1/packages/mypkg
```

Delete a version:

```bash
curl -X DELETE \
  -H "Authorization: Bearer dev-token" \
  http://localhost:8080/api/v1/artifacts/mypkg/1.0.0
```

Run garbage collection:

```bash
curl -X POST \
  -H "Authorization: Bearer dev-token" \
  http://localhost:8080/api/v1/gc
```

## CLI Usage

```bash
registry-cli push mypkg 1.0.0 ./file.tar.gz --server http://localhost:8080 --token dev-token
registry-cli pull mypkg 1.0.0 --output ./file.tar.gz --server http://localhost:8080 --token dev-token
registry-cli list --server http://localhost:8080 --token dev-token
registry-cli search mypkg --server http://localhost:8080 --token dev-token
registry-cli delete mypkg 1.0.0 --server http://localhost:8080 --token dev-token
```

## Storage Design

Blobs are stored as:

```text
<dataDir>/blobs/<first2>/<full_sha256_hash>
```

Uploads are streamed into a temp file first, hashed during write, then atomically renamed into the final content-addressed path.

## SQLite Schema

```sql
CREATE TABLE packages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT UNIQUE NOT NULL
);

CREATE TABLE artifacts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  package_id INTEGER NOT NULL,
  version TEXT NOT NULL,
  hash TEXT NOT NULL,
  size INTEGER NOT NULL,
  uploaded_at DATETIME NOT NULL,
  UNIQUE(package_id, version),
  FOREIGN KEY (package_id) REFERENCES packages(id)
);
```

## Example End-to-End Demo

```bash
# 1) Start server
./registry-server -config ./config.yaml

# 2) Push artifact
registry-cli push demo 1.0.0 ./demo.bin --token dev-token

# 3) Verify registry contents
registry-cli list --token dev-token
registry-cli search demo --token dev-token

# 4) Pull artifact
registry-cli pull demo 1.0.0 --output ./downloaded-demo.bin --token dev-token

# 5) Delete version then collect orphaned blobs
registry-cli delete demo 1.0.0 --token dev-token
curl -X POST -H "Authorization: Bearer dev-token" http://localhost:8080/api/v1/gc
```

## Testing

```bash
go test ./...
```
