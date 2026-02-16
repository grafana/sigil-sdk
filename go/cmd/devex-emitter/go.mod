module github.com/grafana/sigil/sdks/go/cmd/devex-emitter

go 1.25.6

require (
	github.com/anthropics/anthropic-sdk-go v1.22.1
	github.com/grafana/sigil/sdks/go v0.0.0
	github.com/grafana/sigil/sdks/go-providers/anthropic v0.0.0
	github.com/grafana/sigil/sdks/go-providers/gemini v0.0.0
	github.com/grafana/sigil/sdks/go-providers/openai v0.0.0
	github.com/openai/openai-go/v3 v3.21.0
	google.golang.org/genai v1.46.0
)

require (
	cloud.google.com/go v0.116.0 // indirect
	cloud.google.com/go/auth v0.9.3 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.40.0 // indirect
	go.opentelemetry.io/otel/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	golang.org/x/crypto v0.47.0 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260128011058-8636f8732409 // indirect
	google.golang.org/grpc v1.78.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/grafana/sigil/sdks/go => ../..

replace github.com/grafana/sigil/sdks/go-providers/anthropic => ../../../go-providers/anthropic

replace github.com/grafana/sigil/sdks/go-providers/gemini => ../../../go-providers/gemini

replace github.com/grafana/sigil/sdks/go-providers/openai => ../../../go-providers/openai
