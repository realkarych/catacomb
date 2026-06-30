package config

import "fmt"

func Validate(c Config) error {
	if err := validateStore(c.Store); err != nil {
		return err
	}
	if err := validateSources(c.Sources); err != nil {
		return err
	}
	return validateSinks(c.Sinks)
}

func validateStore(s StoreConfig) error {
	switch s.Backend {
	case BackendSQLite:
		if s.SQLite.Path == "" {
			return fmt.Errorf("config.Validate: %w", ErrMissingSQLitePath)
		}
		return nil
	case BackendMemory:
		return nil
	case BackendPostgres:
		return fmt.Errorf("config.Validate: %w", ErrBackendNotImplemented)
	case "":
		return fmt.Errorf("config.Validate: %w", ErrNoStoreBackend)
	default:
		return fmt.Errorf("config.Validate: %w", ErrUnknownStoreBackend)
	}
}

func validateSources(s SourcesConfig) error {
	if s.JSONL.Enabled != nil && *s.JSONL.Enabled && s.JSONL.TranscriptDir == "" {
		return fmt.Errorf("config.Validate: %w", ErrEmptyTranscriptDir)
	}
	return nil
}

func validateSinks(sinks []Sink) error {
	seen := map[string]struct{}{}
	for _, sk := range sinks {
		target, err := sinkTarget(sk)
		if err != nil {
			return err
		}
		key := sk.Type + "|" + target
		if _, dup := seen[key]; dup {
			return fmt.Errorf("config.Validate: %w", ErrDuplicateSink)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func SinkKey(s Sink) string {
	switch s.Type {
	case SinkPostgres:
		return s.Type + "|" + s.DSN
	case SinkNeo4j:
		return s.Type + "|" + s.URI
	case SinkOTLP:
		return s.Type + "|" + s.Endpoint
	case SinkJSONL:
		return s.Type + "|" + s.Path
	default:
		return s.Type
	}
}

func sinkTarget(sk Sink) (string, error) {
	switch sk.Type {
	case SinkPostgres:
		if sk.DSN == "" {
			return "", fmt.Errorf("config.Validate: %w", ErrMissingSinkField)
		}
		return sk.DSN, nil
	case SinkNeo4j:
		if sk.URI == "" || sk.User == "" || sk.Password == "" {
			return "", fmt.Errorf("config.Validate: %w", ErrMissingSinkField)
		}
		return sk.URI, nil
	case SinkOTLP:
		if sk.Endpoint == "" {
			return "", fmt.Errorf("config.Validate: %w", ErrMissingSinkField)
		}
		return sk.Endpoint, nil
	case SinkJSONL:
		if sk.Path == "" {
			return "", fmt.Errorf("config.Validate: %w", ErrMissingSinkField)
		}
		return sk.Path, nil
	default:
		return "", fmt.Errorf("config.Validate: %w", ErrUnknownSink)
	}
}
