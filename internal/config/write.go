package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// SetKeyringCommand writes os.keyring_command into the config file at path (DefaultPath when empty),
// creating the file if needed. Other keys and comments in an existing file are preserved.
// It returns the path written to.
func SetKeyringCommand(path, command string) (string, error) {
	if path == "" {
		var err error
		if path, err = DefaultPath(); err != nil {
			return "", err
		}
	}

	var doc yaml.Node
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return "", fmt.Errorf("reading config %s: %w", path, err)
	default:
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return "", fmt.Errorf("parsing config %s: %w", path, err)
		}
	}

	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return "", fmt.Errorf("config %s: top level is not a mapping", path)
	}

	section := mappingValue(root, "os")
	if section.Kind != yaml.MappingNode {
		// "os:" without content parses as a null scalar.
		*section = yaml.Node{Kind: yaml.MappingNode}
	}
	*mappingValue(section, "keyring_command") = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: command}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", fmt.Errorf("encoding config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("encoding config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("creating config directory: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return "", fmt.Errorf("writing config %s: %w", path, err)
	}
	return path, nil
}

// mappingValue returns the value node for key in mapping m, appending an empty one if missing.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	v := &yaml.Node{Kind: yaml.MappingNode}
	m.Content = append(m.Content, k, v)
	return v
}
