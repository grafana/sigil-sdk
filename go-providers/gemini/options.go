package gemini

// Option configures Gemini mapper behavior.
type Option func(*mapperOptions)

type mapperOptions struct {
	providerName   string
	conversationID string
	tags           map[string]string
	metadata       map[string]any

	includeRequestArtifact  bool
	includeResponseArtifact bool
	includeToolsArtifact    bool
	includeEventsArtifact   bool
}

func defaultOptions() mapperOptions {
	return mapperOptions{
		providerName:            "gemini",
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

// WithProviderName overrides the provider name stored in Generation.Model.Provider.
func WithProviderName(provider string) Option {
	return func(options *mapperOptions) {
		options.providerName = provider
	}
}

// WithConversationID sets Generation.ConversationID.
func WithConversationID(conversationID string) Option {
	return func(options *mapperOptions) {
		options.conversationID = conversationID
	}
}

// WithTags merges all provided tags into Generation.Tags.
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

// WithTag sets one key/value tag in Generation.Tags.
func WithTag(key, value string) Option {
	return func(options *mapperOptions) {
		if options.tags == nil {
			options.tags = map[string]string{}
		}
		options.tags[key] = value
	}
}

// WithMetadata merges all provided values into Generation.Metadata.
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

// WithoutRequestArtifact disables request artifact emission.
func WithoutRequestArtifact() Option {
	return func(options *mapperOptions) {
		options.includeRequestArtifact = false
	}
}

// WithoutResponseArtifact disables response artifact emission.
func WithoutResponseArtifact() Option {
	return func(options *mapperOptions) {
		options.includeResponseArtifact = false
	}
}

// WithoutToolsArtifact disables tools artifact emission.
func WithoutToolsArtifact() Option {
	return func(options *mapperOptions) {
		options.includeToolsArtifact = false
	}
}

// WithoutEventsArtifact disables stream events artifact emission.
func WithoutEventsArtifact() Option {
	return func(options *mapperOptions) {
		options.includeEventsArtifact = false
	}
}
