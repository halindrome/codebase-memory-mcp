package lang

import "testing"

func TestRegisterExtension_KnownLanguage(t *testing.T) {
	t.Cleanup(func() { delete(registry, ".mypy") })
	RegisterExtension(".mypy", Python)
	lang, ok := LanguageForExtension(".mypy")
	if !ok {
		t.Fatal("LanguageForExtension(.mypy) returned false, want true")
	}
	if lang != Python {
		t.Errorf("LanguageForExtension(.mypy) = %v, want %v", lang, Python)
	}
}

func TestRegisterExtension_UnknownLanguage(t *testing.T) {
	t.Cleanup(func() { delete(registry, ".xyz") })
	RegisterExtension(".xyz", Language("notreal"))
	_, ok := LanguageForExtension(".xyz")
	if ok {
		t.Error("LanguageForExtension(.xyz) returned true for unknown language, want false")
	}
}

func TestRegisterExtension_OverridesBuiltin(t *testing.T) {
	t.Cleanup(func() { RegisterExtension(".js", JavaScript) })
	RegisterExtension(".js", Python)
	lang, ok := LanguageForExtension(".js")
	if !ok || lang != Python {
		t.Errorf("expected .js→Python after override, got %v, %v", lang, ok)
	}
}
