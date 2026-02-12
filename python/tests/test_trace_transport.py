"""OTLP trace transport tests (HTTP and gRPC)."""

from __future__ import annotations

from http.server import BaseHTTPRequestHandler, HTTPServer
import socket
import threading
from datetime import timedelta

import grpc
from opentelemetry.proto.collector.trace.v1 import trace_service_pb2, trace_service_pb2_grpc

from sigil_sdk import (
    AuthConfig,
    Client,
    ClientConfig,
    GenerationExportConfig,
    GenerationStart,
    Message,
    MessageRole,
    ModelRef,
    Part,
    PartKind,
    TraceConfig,
)
from sigil_sdk.models import ExportGenerationResult, ExportGenerationsResponse


class _NoopGenerationExporter:
    def export_generations(self, request):
        return ExportGenerationsResponse(
            results=[
                ExportGenerationResult(generation_id=g.id, accepted=True)
                for g in request.generations
            ]
        )

    def shutdown(self) -> None:
        return


class _HTTPTraceHandler(BaseHTTPRequestHandler):
    requests = []
    headers = []

    def do_POST(self):  # noqa: N802
        length = int(self.headers.get("Content-Length", "0"))
        payload = self.rfile.read(length)

        request = trace_service_pb2.ExportTraceServiceRequest()
        request.ParseFromString(payload)
        _HTTPTraceHandler.requests.append(request)
        _HTTPTraceHandler.headers.append({k.lower(): v for k, v in self.headers.items()})

        response = trace_service_pb2.ExportTraceServiceResponse()
        encoded = response.SerializeToString()
        self.send_response(200)
        self.send_header("Content-Type", "application/x-protobuf")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def log_message(self, _format, *_args):  # noqa: A003
        return


class _GRPCTraceServicer(trace_service_pb2_grpc.TraceServiceServicer):
    def __init__(self) -> None:
        self.requests = []
        self.metadata = []

    def Export(self, request, context):  # noqa: N802
        self.requests.append(request)
        self.metadata.append({item.key: item.value for item in context.invocation_metadata()})
        return trace_service_pb2.ExportTraceServiceResponse()


def test_sdk_trace_export_over_http() -> None:
    _HTTPTraceHandler.requests = []
    _HTTPTraceHandler.headers = []
    server = HTTPServer(("127.0.0.1", 0), _HTTPTraceHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()

    client = Client(
        ClientConfig(
            trace=TraceConfig(
                protocol="http",
                endpoint=f"http://127.0.0.1:{server.server_address[1]}/v1/traces",
                insecure=True,
            ),
            generation_export=GenerationExportConfig(batch_size=1, flush_interval=timedelta(seconds=1)),
            generation_exporter=_NoopGenerationExporter(),
        )
    )

    try:
        rec = client.start_generation(
            GenerationStart(
                id="gen-trace-http",
                conversation_id="conv-trace-http",
                agent_name="trace-agent-http",
                agent_version="trace-v-http",
                model=ModelRef(provider="openai", name="gpt-5"),
            )
        )
        rec.set_result(
            output=[
                Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="hi")]),
            ]
        )
        rec.end()
        assert rec.err() is None

        client.shutdown()

        assert len(_HTTPTraceHandler.requests) == 1
        _assert_trace_request_for_generation(_HTTPTraceHandler.requests[0], rec.last_generation)
    finally:
        server.shutdown()
        server.server_close()


def test_sdk_trace_export_over_grpc() -> None:
    servicer = _GRPCTraceServicer()
    grpc_server = grpc.server(thread_pool=__import__("concurrent.futures").futures.ThreadPoolExecutor(max_workers=2))
    trace_service_pb2_grpc.add_TraceServiceServicer_to_server(servicer, grpc_server)

    sock = socket.socket()
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
    sock.close()

    grpc_server.add_insecure_port(f"127.0.0.1:{port}")
    grpc_server.start()

    client = Client(
        ClientConfig(
            trace=TraceConfig(
                protocol="grpc",
                endpoint=f"127.0.0.1:{port}",
                insecure=True,
            ),
            generation_export=GenerationExportConfig(batch_size=1, flush_interval=timedelta(seconds=1)),
            generation_exporter=_NoopGenerationExporter(),
        )
    )

    try:
        rec = client.start_streaming_generation(
            GenerationStart(
                id="gen-trace-grpc",
                conversation_id="conv-trace-grpc",
                agent_name="trace-agent-grpc",
                agent_version="trace-v-grpc",
                model=ModelRef(provider="anthropic", name="claude-sonnet-4-5"),
            )
        )
        rec.set_result(
            output=[
                Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="hi")]),
            ]
        )
        rec.end()
        assert rec.err() is None

        client.shutdown()

        assert len(servicer.requests) == 1
        _assert_trace_request_for_generation(servicer.requests[0], rec.last_generation)
    finally:
        grpc_server.stop(grace=0)


def test_sdk_trace_auth_bearer_over_http() -> None:
    _HTTPTraceHandler.requests = []
    _HTTPTraceHandler.headers = []
    server = HTTPServer(("127.0.0.1", 0), _HTTPTraceHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()

    client = Client(
        ClientConfig(
            trace=TraceConfig(
                protocol="http",
                endpoint=f"http://127.0.0.1:{server.server_address[1]}/v1/traces",
                insecure=True,
                auth=AuthConfig(mode="bearer", bearer_token="trace-secret"),
            ),
            generation_export=GenerationExportConfig(batch_size=1, flush_interval=timedelta(seconds=1)),
            generation_exporter=_NoopGenerationExporter(),
        )
    )

    try:
        rec = client.start_generation(
            GenerationStart(
                id="gen-trace-http-auth",
                conversation_id="conv-trace-http-auth",
                model=ModelRef(provider="openai", name="gpt-5"),
            )
        )
        rec.set_result(output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="hi")])])
        rec.end()
        assert rec.err() is None
        client.shutdown()

        assert len(_HTTPTraceHandler.requests) == 1
        assert len(_HTTPTraceHandler.headers) == 1
        assert _HTTPTraceHandler.headers[0].get("authorization") == "Bearer trace-secret"
    finally:
        server.shutdown()
        server.server_close()


def test_sdk_trace_auth_tenant_over_grpc_with_header_override() -> None:
    servicer = _GRPCTraceServicer()
    grpc_server = grpc.server(thread_pool=__import__("concurrent.futures").futures.ThreadPoolExecutor(max_workers=2))
    trace_service_pb2_grpc.add_TraceServiceServicer_to_server(servicer, grpc_server)

    sock = socket.socket()
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
    sock.close()

    grpc_server.add_insecure_port(f"127.0.0.1:{port}")
    grpc_server.start()

    client = Client(
        ClientConfig(
            trace=TraceConfig(
                protocol="grpc",
                endpoint=f"127.0.0.1:{port}",
                insecure=True,
                headers={"x-scope-orgid": "override-tenant"},
                auth=AuthConfig(mode="tenant", tenant_id="tenant-a"),
            ),
            generation_export=GenerationExportConfig(batch_size=1, flush_interval=timedelta(seconds=1)),
            generation_exporter=_NoopGenerationExporter(),
        )
    )

    try:
        rec = client.start_generation(
            GenerationStart(
                id="gen-trace-grpc-auth",
                conversation_id="conv-trace-grpc-auth",
                model=ModelRef(provider="anthropic", name="claude-sonnet-4-5"),
            )
        )
        rec.set_result(output=[Message(role=MessageRole.ASSISTANT, parts=[Part(kind=PartKind.TEXT, text="hi")])])
        rec.end()
        assert rec.err() is None
        client.shutdown()

        assert len(servicer.requests) == 1
        assert len(servicer.metadata) == 1
        assert servicer.metadata[0].get("x-scope-orgid") == "override-tenant"
    finally:
        grpc_server.stop(grace=0)


def _assert_trace_request_for_generation(request, generation) -> None:
    assert generation is not None

    span = None
    for resource_spans in request.resource_spans:
        for scope_spans in resource_spans.scope_spans:
            for candidate in scope_spans.spans:
                if candidate.name == f"{generation.operation_name} {generation.model.name}":
                    span = candidate
                    break

    assert span is not None

    attrs = _attr_string_map(span.attributes)
    assert attrs.get("sigil.generation.id") == generation.id
    assert attrs.get("gen_ai.conversation.id") == generation.conversation_id
    assert attrs.get("gen_ai.agent.name") == generation.agent_name
    assert attrs.get("gen_ai.agent.version") == generation.agent_version
    assert attrs.get("gen_ai.provider.name") == generation.model.provider
    assert attrs.get("gen_ai.request.model") == generation.model.name
    assert attrs.get("gen_ai.operation.name") == generation.operation_name


def _attr_string_map(attrs) -> dict[str, str]:
    out = {}
    for kv in attrs:
        if kv.value.HasField("string_value"):
            out[kv.key] = kv.value.string_value
    return out
