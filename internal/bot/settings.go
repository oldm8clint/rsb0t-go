package bot

import "rsb0t-go/internal/settings"

// SettingsBag ports AbstractBot.settings: the account's script settings (Context.Settings) as a
// settings.Bag with the typed Bool/Num/Int/Str/List/Tile accessors. The Bag is a copy, so
// changing it never touches Context.Settings.
func (c *Context) SettingsBag() *settings.Bag {
	return settings.NewBagFrom(c.Settings)
}

// ResolveSettings resolves Context.Settings against a script's declared schema: defaults for
// missing keys, numbers clamped, options matched, tiles parsed (see settings.Schema.Resolve).
func (c *Context) ResolveSettings(schema settings.Schema) *settings.Bag {
	return schema.Resolve(c.Settings)
}
