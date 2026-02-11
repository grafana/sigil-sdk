package openai

type Option func(*mapperOptions)

type mapperOptions struct {
	providerName string
	threadID     string
	tags         map[string]string
	metadata     map[string]any

	includeRequestArtifact  bool
	includeResponseArtifact bool
	includeToolsArtifact    bool
	includeEventsArtifact   bool
}

func defaultOptions() mapperOptions {
	return mapperOptions{
		providerName:            "openai",
		includeRequestArtifact:  true,
		includeResponseArtifact: true,
		includeToolsArtifact:    true,
		includeEventsArtifact:   true,
	}
}

func applyOptions(options []Option) mapperOptions {
	out := defaultOptions()
	for _, option := range options {
		option(&out)
	}
	return out
}

func WithProviderName(provider string) Option {
	return func(options *mapperOptions) {
		options.providerName = provider
	}
}

func WithThreadID(threadID string) Option {
	return func(options *mapperOptions) {
		options.threadID = threadID
	}
}

func WithTags(tags map[string]string) Option {
	return func(options *mapperOptions) {
		if len(tags) == 0 {
			return
		}
		if options.tags == nil {
			options.tags = make(map[string]string, len(tags))
		}
		for key, value := range tags {
			options.tags[key] = value
		}
	}
}

func WithTag(key, value string) Option {
	return func(options *mapperOptions) {
		if options.tags == nil {
			options.tags = map[string]string{}
		}
		options.tags[key] = value
	}
}

func WithMetadata(metadata map[string]any) Option {
	return func(options *mapperOptions) {
		if len(metadata) == 0 {
			return
		}
		if options.metadata == nil {
			options.metadata = make(map[string]any, len(metadata))
		}
		for key, value := range metadata {
			options.metadata[key] = value
		}
	}
}

func WithoutRequestArtifact() Option {
	return func(options *mapperOptions) {
		options.includeRequestArtifact = false
	}
}

func WithoutResponseArtifact() Option {
	return func(options *mapperOptions) {
		options.includeResponseArtifact = false
	}
}

func WithoutToolsArtifact() Option {
	return func(options *mapperOptions) {
		options.includeToolsArtifact = false
	}
}

func WithoutEventsArtifact() Option {
	return func(options *mapperOptions) {
		options.includeEventsArtifact = false
	}
}
