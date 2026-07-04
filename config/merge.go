package config

func Merge(base, override Config) Config {
	out := base
	out.Daemon = mergeDaemon(base.Daemon, override.Daemon)
	out.Store = mergeStore(base.Store, override.Store)
	out.Sources = mergeSources(base.Sources, override.Sources)
	if override.Sinks != nil {
		out.Sinks = override.Sinks
	}
	out.Payloads = mergePayloads(base.Payloads, override.Payloads)
	return out
}

func mergePayloads(base, o PayloadsConfig) PayloadsConfig {
	if o.Mode != "" {
		base.Mode = o.Mode
	}
	if o.MaxBytes != 0 {
		base.MaxBytes = o.MaxBytes
	}
	return base
}

func mergeDaemon(base, o DaemonConfig) DaemonConfig {
	if o.Discovery != "" {
		base.Discovery = o.Discovery
	}
	if o.ReaperWindow != 0 {
		base.ReaperWindow = o.ReaperWindow
	}
	if o.MaxShards != 0 {
		base.MaxShards = o.MaxShards
	}
	if o.AllowPayloadAccess {
		base.AllowPayloadAccess = true
	}
	if o.AllowAnnotations {
		base.AllowAnnotations = true
	}
	return base
}

func mergeStore(base, o StoreConfig) StoreConfig {
	if o.Backend != "" {
		base.Backend = o.Backend
	}
	if o.SQLite.Path != "" {
		base.SQLite.Path = o.SQLite.Path
	}
	if o.Postgres.DSN != "" {
		base.Postgres.DSN = o.Postgres.DSN
	}
	return base
}

func mergeSources(base, o SourcesConfig) SourcesConfig {
	base.Hooks = mergeToggle(base.Hooks, o.Hooks)
	base.Otel = mergeToggle(base.Otel, o.Otel)
	base.StreamJSON = mergeToggle(base.StreamJSON, o.StreamJSON)
	base.JSONL = mergeJSONL(base.JSONL, o.JSONL)
	return base
}

func mergeToggle(base, o SourceToggle) SourceToggle {
	if o.Enabled != nil {
		base.Enabled = o.Enabled
	}
	return base
}

func mergeJSONL(base, o JSONLSource) JSONLSource {
	if o.Enabled != nil {
		base.Enabled = o.Enabled
	}
	if o.TranscriptDir != "" {
		base.TranscriptDir = o.TranscriptDir
	}
	if o.Exclude != nil {
		base.Exclude = o.Exclude
	}
	return base
}
