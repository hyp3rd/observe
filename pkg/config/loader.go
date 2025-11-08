package config

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hyp3rd/ewrap"
	"github.com/mitchellh/mapstructure"
	"gopkg.in/yaml.v3"
)

const errMsgUnableToReadConfigFromPath = "read config file %q"

// Loader transforms external sources into configuration maps that are decoded onto Config.
type Loader interface {
	Load(ctx context.Context) (map[string]any, error)
}

// LoaderFunc adapts ordinary functions into Loader.
type LoaderFunc func(ctx context.Context) (map[string]any, error)

// Load implements Loader.
func (lf LoaderFunc) Load(ctx context.Context) (map[string]any, error) {
	return lf(ctx)
}

type loaderSkipError struct {
	err *ewrap.Error
}

func newLoaderSkipError() error {
	return &loaderSkipError{err: ewrap.New("config loader skip")}
}

// Error implements error.
func (l *loaderSkipError) Error() string {
	if l == nil || l.err == nil {
		return ""
	}

	return l.err.Error()
}

// Unwrap implements errors.Wrapper.
func (l *loaderSkipError) Unwrap() error {
	if l == nil {
		return nil
	}

	return l.err
}

// Is implements errors.Is.
func (*loaderSkipError) Is(target error) bool {
	_, ok := target.(*loaderSkipError)

	return ok
}

func isLoaderSkipError(err error) bool {
	if err == nil {
		return false
	}

	var target *loaderSkipError

	return errors.As(err, &target)
}

// Load runs loaders sequentially, layering their fields over DefaultConfig().
func Load(ctx context.Context, loaders ...Loader) (Config, error) {
	cfg := DefaultConfig()

	for _, loader := range loaders {
		if loader == nil {
			continue
		}

		values, err := loader.Load(ctx)
		if err != nil {
			if isLoaderSkipError(err) {
				continue
			}

			return Config{}, err
		}

		if len(values) == 0 {
			continue
		}

		err = decodeInto(&cfg, values)
		if err != nil {
			return Config{}, ewrap.Wrap(err, "decode config")
		}
	}

	err := Validate(cfg)
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func decodeInto(target *Config, input map[string]any) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		TagName:          "yaml",
		Result:           target,
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.StringToSliceHookFunc(","),
		),
	})
	if err != nil {
		return ewrap.Wrap(err, "create decoder")
	}

	err = decoder.Decode(input)
	if err != nil {
		return ewrap.Wrap(err, "decode config")
	}

	return nil
}

// FileLoader loads configuration from a YAML file.
type FileLoader struct {
	Path string
	FS   fs.FS
}

// Load implements Loader.
func (fl FileLoader) Load(_ context.Context) (map[string]any, error) {
	path := fl.Path
	if path == "" {
		path = "observe.yaml"
	}

	data, err := readFile(fl.FS, path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, newLoaderSkipError()
		}

		return nil, ewrap.Wrapf(err, errMsgUnableToReadConfigFromPath, path)
	}

	var out map[string]any

	err = yaml.Unmarshal(data, &out)
	if err != nil {
		return nil, ewrap.Wrapf(err, "unmarshal yaml %q", path)
	}

	return sanitizeMap(out), nil
}

func readFile(fsys fs.FS, path string) ([]byte, error) {
	if fsys != nil {
		bytes, err := fs.ReadFile(fsys, filepath.Clean(path))
		if err != nil {
			return nil, ewrap.Wrapf(err, errMsgUnableToReadConfigFromPath, path)
		}

		return bytes, nil
	}

	bytes, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, ewrap.Wrapf(err, errMsgUnableToReadConfigFromPath, path)
	}

	return bytes, nil
}

// EnvLoader reads configuration overrides from environment variables.
type EnvLoader struct {
	Prefix string
}

// Load implements Loader.
func (el EnvLoader) Load(ctx context.Context) (map[string]any, error) {
	prefix := el.Prefix
	if prefix == "" {
		prefix = "OBSERVE_"
	}

	pairs := os.Environ()
	result := map[string]any{}

	for _, kv := range pairs {
		select {
		case <-ctx.Done():
			return nil, ewrap.Wrap(ctx.Err(), "context canceled")
		default:
		}

		if !strings.HasPrefix(kv, prefix) {
			continue
		}

		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimPrefix(parts[0], prefix)
		value := parts[1]

		path := envKeyToPath(key)
		if len(path) == 0 {
			continue
		}

		if isListKey(path) {
			result = setNested(result, path, splitList(value))
		} else {
			result = setNested(result, path, value)
		}
	}

	if len(result) == 0 {
		return nil, newLoaderSkipError()
	}

	return result, nil
}

func envKeyToPath(key string) []string {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "__", ".")
	key = strings.ReplaceAll(key, "-", "_")
	segments := strings.Split(key, ".")

	filtered := segments[:0]
	for _, seg := range segments {
		seg = strings.Trim(seg, ".")
		if seg == "" {
			continue
		}

		filtered = append(filtered, seg)
	}

	return filtered
}

func isListKey(path []string) bool {
	switch strings.Join(path, ".") {
	case "instrumentation.http.ignored_routes",
		"instrumentation.grpc.metadata_allowlist":
		return true
	default:
		return false
	}
}

func splitList(raw string) []string {
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")

	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		out = append(out, part)
	}

	return out
}

func setNested(root map[string]any, path []string, value any) map[string]any {
	if root == nil {
		root = map[string]any{}
	}

	cursor := root

	for idx, segment := range path {
		if idx == len(path)-1 {
			cursor[segment] = value

			return root
		}

		next, ok := cursor[segment].(map[string]any)
		if !ok || next == nil {
			next = map[string]any{}
			cursor[segment] = next
		}

		cursor = next
	}

	return root
}

func sanitizeMap(in map[string]any) map[string]any {
	data, err := json.Marshal(in)
	if err != nil {
		return in
	}

	var out map[string]any

	err = json.Unmarshal(data, &out)
	if err != nil {
		return in
	}

	return out
}
