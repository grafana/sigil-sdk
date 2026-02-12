#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SDK_DIR="${ROOT_DIR}/sdks/python"
OUT_DIR="${SDK_DIR}/sigil_sdk/internal/gen"
PYTHON_BIN="${PYTHON_BIN:-python3}"

if ! "${PYTHON_BIN}" -c "import grpc_tools" >/dev/null 2>&1; then
  cat <<'EOF'
grpcio-tools is required to regenerate protobuf stubs.
Install it with:
  python3 -m pip install 'sdks/python[dev]'
or:
  python3 -m pip install grpcio-tools
EOF
  exit 1
fi

PROTO_INCLUDE="$(${PYTHON_BIN} -c 'import pathlib, grpc_tools; print(pathlib.Path(grpc_tools.__file__).parent / "_proto")')"

"${PYTHON_BIN}" -m grpc_tools.protoc \
  -I"${ROOT_DIR}/api/proto" \
  -I"${PROTO_INCLUDE}" \
  --python_out="${OUT_DIR}" \
  --grpc_python_out="${OUT_DIR}" \
  "${ROOT_DIR}/api/proto/sigil/v1/generation_ingest.proto"

# The grpc plugin emits absolute import paths; normalize to relative package import.
TMP_FILE="$(mktemp)"
sed 's|from sigil.v1 import generation_ingest_pb2 as|from . import generation_ingest_pb2 as|' \
  "${OUT_DIR}/sigil/v1/generation_ingest_pb2_grpc.py" > "${TMP_FILE}"
mv "${TMP_FILE}" "${OUT_DIR}/sigil/v1/generation_ingest_pb2_grpc.py"
