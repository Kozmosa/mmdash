package article

import (
	"testing"

	"github.com/mmdash/mmdash/backend/internal/settings"
)

func TestRenderingSettingDefinitionAllowsOnlySupportedThemes(t *testing.T) {
	definition := SettingDefinitionRendering()
	if definition.Key != SettingTypeRendering || definition.Validator == nil || len(definition.Fields) != 2 {
		t.Fatalf("unexpected rendering definition: %#v", definition)
	}
	if err := definition.Validator.ValidateConfig(map[string]interface{}{"theme": "latex"}); err != nil {
		t.Fatal(err)
	}
	if err := definition.Validator.ValidateConfig(map[string]interface{}{"theme": "other"}); err == nil {
		t.Fatal("unsupported rendering theme was accepted")
	}
}

func TestRenderingSettingSplitSectionsIsOptionalBoolean(t *testing.T) {
	validator := renderingSettingValidator{}
	if err := validator.ValidateConfig(map[string]interface{}{
		"theme": "md", "split_sections": false,
	}); err != nil {
		t.Fatalf("explicit split_sections opt-out rejected: %v", err)
	}
	// Settings stored before the field existed stay valid.
	if err := validator.ValidateConfig(map[string]interface{}{"theme": "md"}); err != nil {
		t.Fatalf("legacy rendering setting rejected: %v", err)
	}
	if err := validator.ValidateConfig(map[string]interface{}{
		"theme": "md", "split_sections": "yes",
	}); err == nil {
		t.Fatal("non-boolean split_sections accepted")
	}
}

func TestResolveSplitSectionsDefaultsToEnabled(t *testing.T) {
	if enabled := ResolveSplitSections(settings.ResolvedSetting{}, false); !enabled {
		t.Fatal("missing setting must default to splitting enabled")
	}
	if enabled := ResolveSplitSections(settings.ResolvedSetting{
		Values: map[string]interface{}{"theme": "md"},
	}, true); !enabled {
		t.Fatal("setting without split_sections must default to enabled")
	}
	if enabled := ResolveSplitSections(settings.ResolvedSetting{
		Values: map[string]interface{}{"split_sections": false},
	}, true); enabled {
		t.Fatal("explicit false must disable splitting")
	}
}
