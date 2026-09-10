package article

import (
	"fmt"

	"github.com/mmdash/mmdash/backend/internal/settings"
)

const SettingTypeRendering = "article.rendering"

type renderingSettingValidator struct{}

func SettingDefinitionRendering() settings.TypeDefinition {
	return settings.TypeDefinition{
		Description: "Selects the shared Markdown rendering theme and LaTeX body layout for the Project Article workspace.",
		Fields: []settings.FieldDefinition{
			{Key: "theme", Kind: settings.FieldSelect, Label: "Rendering theme", Options: []string{"md", "latex"}, Required: true},
			{Key: "split_sections", Kind: settings.FieldBoolean, Label: "Split sections into TeX files", Required: false},
		},
		Key: SettingTypeRendering, Order: 66, Owner: "article",
		Scopes: []settings.Scope{settings.ScopeProject},
		Title:  "Article rendering", Validator: renderingSettingValidator{},
	}
}

func (renderingSettingValidator) ValidateConfig(values map[string]interface{}) error {
	theme, _ := values["theme"].(string)
	if theme != "md" && theme != "latex" {
		return fmt.Errorf("%w: unsupported Article rendering theme", ErrInvalid)
	}
	// split_sections is optional: builds fall back to the product default
	// (enabled) when the stored setting predates the field.
	if raw, exists := values["split_sections"]; exists {
		if _, ok := raw.(bool); !ok {
			return fmt.Errorf("%w: split_sections must be a boolean", ErrInvalid)
		}
	}
	return nil
}

// ResolveSplitSections reports whether Article builds should split every H1
// section into its own TeX file. The product default is enabled; only an
// explicit false opt out.
func ResolveSplitSections(resolved settings.ResolvedSetting, found bool) bool {
	if !found {
		return true
	}
	value, exists := resolved.Values["split_sections"]
	if !exists {
		return true
	}
	enabled, ok := value.(bool)
	return !ok || enabled
}
