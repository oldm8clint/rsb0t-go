package bot

import "testing"

func TestEverySchemaBelongsToARegisteredScript(t *testing.T) {
	for name, sc := range scriptSchemas {
		if _, ok := New(name); !ok {
			t.Errorf("%s has a settings schema but is not a registered script", name)
		}
		if len(sc) == 0 {
			t.Errorf("%s has an empty settings schema", name)
		}
		if got, ok := SettingsSchema(name); !ok || len(got) != len(sc) {
			t.Errorf("SettingsSchema(%q) did not return its schema", name)
		}
	}
	if _, ok := SettingsSchema("ProgressiveFisher"); ok {
		t.Error("a script with no typed schema reported one")
	}
}
