// schema.go
//
// Parses yaga.yaml into the typed config schema. ParseFile reads the file
// from disk and Parse decodes raw YAML bytes; both run the validator so only
// valid configurations are returned.
package parser

import (
	"fmt"
	"os"

	"github.com/MichalHerstus/yaga/internal/types"
	"gopkg.in/yaml.v3"
)

// ParseFile reads the YAML config at the given path and parses/validates it.
// Params: path (filesystem path of the yaga.yaml file).
// Returns: the parsed *types.Config, or an error if reading, decoding or
// validation fails.
func ParseFile(path string) (*types.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	return Parse(data)
}

// Parse decodes raw YAML bytes into a *types.Config using yaml.v3 and then
// runs Validate, mutating the config to fill in defaulted values.
// Params: data (raw YAML config bytes).
// Returns: the parsed and validated *types.Config, or an error.
func Parse(data []byte) (*types.Config, error) {
	var cfg types.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing yaml: %w", err)
	}
	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}
	return &cfg, nil
}
