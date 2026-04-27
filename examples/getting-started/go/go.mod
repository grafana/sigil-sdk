module github.com/grafana/sigil-sdk/examples/getting-started/go

go 1.23

require (
	github.com/grafana/sigil-sdk/go v0.0.0
	github.com/openai/openai-go/v3 v3.29.0
)

replace github.com/grafana/sigil-sdk/go => ../../../go
