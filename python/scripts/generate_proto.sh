#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SDK_DIR="${ROOT_DIR}/python"
OUT_DIR="${SDK_DIR}/sigil_sdk/internal/gen"
PYTHON_BIN="${PYTHON_BIN:-python3}"

if "${PYTHON_BIN}" -c "import grpc_tools" >/dev/null 2>&1; then
  PYTHON=("${PYTHON_BIN}")
elif command -v uv >/dev/null 2>&1; then
  PYTHON=(uv run --quiet --directory "${SDK_DIR}" --with grpcio-tools python)
else
  cat >&2 <<'EOF'
grpcio-tools is required to regenerate protobuf stubs.
Either install uv (preferred — https://docs.astral.sh/uv/) and re-run,
or install grpcio-tools into your Python:
  python3 -m pip install grpcio-tools
EOF
  exit 1
fi

PROTO_INCLUDE="$("${PYTHON[@]}" -c 'import pathlib, grpc_tools; print(pathlib.Path(grpc_tools.__file__).parent / "_proto")')"

"${PYTHON[@]}" -m grpc_tools.protoc \
  -I"${ROOT_DIR}/proto" \
  -I"${PROTO_INCLUDE}" \
  --python_out="${OUT_DIR}" \
  --grpc_python_out="${OUT_DIR}" \
  "${ROOT_DIR}/proto/sigil/v1/generation_ingest.proto"

# The grpc plugin emits absolute import paths; normalize to relative package import.
TMP_FILE="$(mktemp)"
sed 's|from sigil.v1 import generation_ingest_pb2 as|from . import generation_ingest_pb2 as|' \
  "${OUT_DIR}/sigil/v1/generation_ingest_pb2_grpc.py" > "${TMP_FILE}"
mv "${TMP_FILE}" "${OUT_DIR}/sigil/v1/generation_ingest_pb2_grpc.py"
