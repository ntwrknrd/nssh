package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type configDocument struct {
	path      string
	root      map[string]any
	effective map[string]any
	sources   map[string]string
}

type configLoadResult struct {
	table   map[string]any
	sources map[string]string
}

func loadConfigDocument(path string) (*configDocument, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	root, err := readTOMLMap(abs)
	if err != nil {
		return nil, err
	}
	result, err := resolveConfigTable(abs, cloneMap(root), "", map[string]bool{abs: true})
	if err != nil {
		return nil, err
	}
	return &configDocument{
		path:      abs,
		root:      root,
		effective: result.table,
		sources:   result.sources,
	}, nil
}

func readTOMLMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var table map[string]any
	if _, err := toml.Decode(string(data), &table); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if table == nil {
		table = make(map[string]any)
	}
	return table, nil
}

func resolveConfigTable(path string, table map[string]any, scope string, stack map[string]bool) (configLoadResult, error) {
	result := configLoadResult{
		table:   make(map[string]any),
		sources: make(map[string]string),
	}

	includes, err := includeValues(table["include"], sourcePath(scope, "include"))
	if err != nil {
		return result, err
	}
	for _, pattern := range includes {
		matches, err := expandInclude(path, pattern)
		if err != nil {
			return result, err
		}
		for _, match := range matches {
			if stack[match] {
				return result, fmt.Errorf("config include cycle: %s", includeCycle(stack, match))
			}
			nextStack := cloneBoolMap(stack)
			nextStack[match] = true
			imported, err := readTOMLMap(match)
			if err != nil {
				return result, err
			}
			resolved, err := resolveConfigTable(match, imported, scope, nextStack)
			if err != nil {
				return result, err
			}
			result = mergeConfigResults(result, resolved)
		}
	}

	local := configLoadResult{
		table:   make(map[string]any),
		sources: make(map[string]string),
	}
	for key, value := range table {
		if key == "include" {
			continue
		}
		keyPath := sourcePath(scope, key)
		local.sources[keyPath] = path
		switch typed := value.(type) {
		case map[string]any:
			resolved, err := resolveConfigTable(path, typed, keyPath, stack)
			if err != nil {
				return result, err
			}
			local.table[key] = resolved.table
			for sourceKey, sourceFile := range resolved.sources {
				local.sources[sourceKey] = sourceFile
			}
		case []any:
			items := make([]any, 0, len(typed))
			for _, item := range typed {
				itemMap, ok := asMap(item)
				if !ok {
					items = append(items, item)
					continue
				}
				resolved, err := resolveConfigTable(path, itemMap, keyPath, stack)
				if err != nil {
					return result, err
				}
				items = append(items, resolved.table)
				for sourceKey, sourceFile := range resolved.sources {
					local.sources[sourceKey] = sourceFile
				}
			}
			local.table[key] = items
		default:
			local.table[key] = value
		}
	}
	return mergeConfigResults(result, local), nil
}

func includeValues(value any, scope string) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("%s must contain only non-empty strings", scope)
			}
			values = append(values, s)
		}
		return values, nil
	case []string:
		return typed, nil
	default:
		return nil, fmt.Errorf("%s must be an array of strings", scope)
	}
}

func expandInclude(fromFile, pattern string) ([]string, error) {
	if strings.TrimSpace(pattern) == "" {
		return nil, fmt.Errorf("config include path is required")
	}
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(filepath.Dir(fromFile), pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid config include pattern %q: %w", pattern, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("config include %q has no matches", pattern)
	}
	sort.Strings(matches)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			return nil, fmt.Errorf("stat config include %s: %w", match, err)
		}
		if info.IsDir() {
			continue
		}
		abs, err := filepath.Abs(match)
		if err != nil {
			return nil, err
		}
		out = append(out, abs)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("config include %q has no file matches", pattern)
	}
	return out, nil
}

func mergeConfigResults(base, override configLoadResult) configLoadResult {
	if base.table == nil {
		base.table = make(map[string]any)
	}
	if base.sources == nil {
		base.sources = make(map[string]string)
	}
	base.table = mergeConfigMaps(base.table, override.table)
	for key, value := range override.sources {
		base.sources[key] = value
	}
	return base
}

func mergeConfigMaps(base, override map[string]any) map[string]any {
	out := cloneMap(base)
	for key, value := range override {
		if baseMap, ok := asMap(out[key]); ok {
			if overrideMap, ok := asMap(value); ok {
				out[key] = mergeConfigMaps(baseMap, overrideMap)
				continue
			}
		}
		out[key] = cloneValue(value)
	}
	return out
}

func decodeConfigDocument(path string, doc *configDocument, cfg *Config) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(doc.effective); err != nil {
		return fmt.Errorf("encode merged config %s: %w", path, err)
	}
	md, err := toml.Decode(buf.String(), cfg)
	if err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, key := range undecoded {
			keys = append(keys, strings.Join(key, "."))
		}
		sort.Strings(keys)
		return fmt.Errorf("validate config %s: unknown config key %s", path, strings.Join(keys, ", "))
	}
	return nil
}

func (c *Config) InventoryGroupSource(name string) string {
	return c.sourceFor("inventory", "group", name)
}

func (c *Config) InventoryHostSource(name string) string {
	return c.sourceFor("inventory", "host", name)
}

func (c *Config) sourceFor(parts ...string) string {
	if c == nil || c.document == nil {
		return ""
	}
	return c.document.sources[strings.Join(parts, ".")]
}

func tablePathDefined(table map[string]any, parts ...string) bool {
	var current any = table
	for _, part := range parts {
		m, ok := asMap(current)
		if !ok {
			return false
		}
		current, ok = m[part]
		if !ok {
			return false
		}
	}
	return true
}

func sourcePath(scope, key string) string {
	if scope == "" {
		return key
	}
	return scope + "." + key
}

func asMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	default:
		return nil, false
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return value
	}
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func includeCycle(stack map[string]bool, repeated string) string {
	paths := make([]string, 0, len(stack)+1)
	for path := range stack {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	paths = append(paths, repeated)
	return strings.Join(paths, " -> ")
}
