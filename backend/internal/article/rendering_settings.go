package article

import (
	"fmt"

	"github.com/mmdash/mmdash/backend/internal/settings"
)

const SettingTypeRendering = "article.rendering"

type renderingSettingValidator struct{}

func SettingDefinitionRendering() settings.TypeDefinition {
	return settings.TypeDefinition{
		Description: "Selects the shared Markdown rendering theme for the Project Article workspace.",
		Fields: []settings.FieldDefinition{
			{Key: "theme", Kind: settings.FieldSelect, Label: "Rendering theme", Options: []string{"md", "latex"}, Required: true},
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
	return nil
}
