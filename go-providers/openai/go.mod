module github.com/grafana/sigil/sdks/go-providers/openai

go 1.25.6

require (
	github.com/grafana/sigil/sdks/go v0.0.0
	github.com/openai/openai-go v1.12.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.opentelemetry.io/otel v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
)

replace github.com/grafana/sigil/sdks/go => ../../go
