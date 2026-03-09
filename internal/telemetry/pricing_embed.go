package telemetry

import (
	_ "embed"
	"fmt"

	"github.com/pelletier/go-toml/v2"

	"github.com/canhta/foreman/internal/models"
)

//go:embed pricing.toml
var embeddedPricingTOML []byte

// pricingFile is the schema we decode the embedded TOML into.
type pricingFile struct {
	Pricing map[string]models.PricingConfig `toml:"pricing"`
}

// LoadEmbeddedPricing parses the embedded pricing.toml and returns the map.
// Returns an error only if the embedded file is malformed (should never happen
// in a correctly built binary).
func LoadEmbeddedPricing() (map[string]models.PricingConfig, error) {
	var f pricingFile
	if err := toml.Unmarshal(embeddedPricingTOML, &f); err != nil {
		return nil, fmt.Errorf("parse embedded pricing.toml: %w", err)
	}
	return f.Pricing, nil
}
