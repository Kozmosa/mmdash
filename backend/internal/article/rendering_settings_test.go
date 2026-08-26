package article

import "testing"

func TestRenderingSettingDefinitionAllowsOnlySupportedThemes(t *testing.T) {
	definition := SettingDefinitionRendering()
	if definition.Key != SettingTypeRendering || definition.Validator == nil || len(definition.Fields) != 1 {
		t.Fatalf("unexpected rendering definition: %#v", definition)
	}
	if err := definition.Validator.ValidateConfig(map[string]interface{}{"theme": "latex"}); err != nil {
		t.Fatal(err)
	}
	if err := definition.Validator.ValidateConfig(map[string]interface{}{"theme": "other"}); err == nil {
		t.Fatal("unsupported rendering theme was accepted")
	}
}
