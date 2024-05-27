package discord

// these are partial structs
// see https://discord.com/developers/docs/interactions/receiving-and-responding#interaction-object

type InteractionOption struct {
	Name  string `json:"name"`
	Type  int    `json:"type"`
	Value string `json:"value"`
}

type InteractionData struct {
	Name    string              `json:"name"`
	Options []InteractionOption `json:"options"`
}

type Interaction struct {
	Data      InteractionData `json:"data"`
	GuildID   string          `json:"guild_id"`
	ChannelID string          `json:"channel_id"`
}

type Channel struct {
	ID      string `json:"id"`
	Type    int    `json:"type"`
	GuildID string `json:"guild_id"`
	Name    string `json:"name"`
}
