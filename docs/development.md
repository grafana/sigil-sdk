# Development

Notes for contributors working in this repo.

## Regenerating protobuf stubs

The canonical proto lives at [`proto/sigil/v1/generation_ingest.proto`](../proto/sigil/v1/generation_ingest.proto). After editing it, regenerate every language's stubs from the repo root:

```bash
mise run generate:proto
```

This runs three subtasks:

| Task | Outputs | Tooling |
| --- | --- | --- |
| `generate:proto:go` | `go/sigil/internal/gen/sigil/v1/*.pb.go` | `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc` |
| `generate:proto:python` | `python/sigil_sdk/internal/gen/sigil/v1/*_pb2*.py` | `grpcio-tools` (auto-fetched via `uv` if needed) |
| `generate:proto:js` | `js/proto/sigil/v1/*.proto` | none — copies the proto for the runtime loader |

Java and .NET compile the proto on build (gradle protobuf plugin and `Grpc.Tools` respectively), so they pick up changes automatically once the canonical `.proto` is updated.

### Go tooling

If `protoc-gen-go` or `protoc-gen-go-grpc` are missing, install them:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

`protoc` itself comes from your package manager (`brew install protobuf`, `apt install protobuf-compiler`, etc.).

### Python tooling

The script prefers an existing Python that has `grpcio-tools` installed (`PYTHON_BIN`, defaults to `python3`); if that's unavailable it falls back to `uv run --with grpcio-tools`. Install [uv](https://docs.astral.sh/uv/) and you don't need to install `grpcio-tools` globally.
