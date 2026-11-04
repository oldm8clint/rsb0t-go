package bot

import "rsb0t-go/internal/settings"

// scriptSchemas are the scripts whose settings are declared as a typed settings.Schema, by registered
// name. A setup page can build a form from these (labels, help, defaults, ranges and choices); every other
// script reads its settings through its own plan package, and is configured as a JSON object.
var scriptSchemas = map[string]settings.Schema{
	"CombatTrainer": CombatTrainerSettings,
	"GemCutter":     GemCutterSettings,
	"HerbCleaner":   HerbCleanerSettings,
	"RuneCarrier":   RuneCarrierSettings,
	"RuneRunner":    RuneRunnerSettings,
	"ShopBuyout":    ShopBuyoutSettings,
	"SmelterBot":    SmelterBotSettings,
}

// SettingsSchema is the typed settings schema of the registered script name, and false for a script
// that declares none.
func SettingsSchema(name string) (settings.Schema, bool) {
	sc, ok := scriptSchemas[name]
	return sc, ok
}
