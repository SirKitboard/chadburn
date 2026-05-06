package middlewares

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/PremoWeb/Chadburn/core"
)

const (
	discordUsername  = "Chadburn"
	discordAvatarURL = "https://raw.githubusercontent.com/PremoWeb/Chadburn/master/static/avatar.png"

	discordColorSuccess = 5763719  // green  (#57F287)
	discordColorFailure = 15548997 // red    (#ED4245)
	discordColorSkipped = 16753920 // orange (#FFA500)
)

// DiscordConfig configuration for the Discord middleware
type DiscordConfig struct {
	DiscordWebhook     string `gcfg:"discord-webhook" mapstructure:"discord-webhook"`
	DiscordOnlyOnError bool   `gcfg:"discord-only-on-error" mapstructure:"discord-only-on-error"`
}

// NewDiscord returns a Discord middleware if the given configuration is not empty
func NewDiscord(c *DiscordConfig) core.Middleware {
	var m core.Middleware
	if !IsEmpty(c) {
		m = &Discord{*c}
	}

	return m
}

// Discord middleware calls a Discord webhook after every execution of a job
type Discord struct {
	DiscordConfig
}

// ContinueOnStop returns always true so we can report the final status
func (m *Discord) ContinueOnStop() bool {
	return true
}

// Run sends a message to the Discord webhook
func (m *Discord) Run(ctx *core.Context) error {
	err := ctx.Run()
	ctx.Stop(err)

	if ctx.Execution.Failed() || !m.DiscordOnlyOnError {
		m.pushMessage(ctx)
	}

	return err
}

func (m *Discord) pushMessage(ctx *core.Context) {
	payload, err := json.Marshal(m.buildPayload(ctx))
	if err != nil {
		ctx.Logger.Errorf("Discord error marshalling payload: %q", err)
		return
	}

	r, err := http.Post(m.DiscordWebhook, "application/json", bytes.NewReader(payload))
	if err != nil {
		ctx.Logger.Errorf("Discord error calling %q error: %q", m.DiscordWebhook, err)
	} else if r.StatusCode < 200 || r.StatusCode >= 300 {
		ctx.Logger.Errorf("Discord error non-2xx status code %d calling %q", r.StatusCode, m.DiscordWebhook)
	}
}

func (m *Discord) buildPayload(ctx *core.Context) *discordWebhookPayload {
	embed := discordEmbed{
		Title:       fmt.Sprintf("Job %q finished", ctx.Job.GetName()),
		Description: fmt.Sprintf("Duration: **%s**", ctx.Execution.Duration()),
		Color:       discordColorSuccess,
	}

	if ctx.Job.GetCommand() != "" {
		embed.Fields = append(embed.Fields, discordField{
			Name:   "Command",
			Value:  fmt.Sprintf("`%s`", ctx.Job.GetCommand()),
			Inline: false,
		})
	}

	if ctx.Execution.Failed() {
		embed.Color = discordColorFailure
		errText := "Unknown error"
		if ctx.Execution.Error() != nil {
			errText = ctx.Execution.Error().Error()
		}
		embed.Fields = append(embed.Fields, discordField{
			Name:   "Error",
			Value:  errText,
			Inline: false,
		})
	} else if ctx.Execution.Skipped() {
		embed.Color = discordColorSkipped
		embed.Fields = append(embed.Fields, discordField{
			Name:   "Status",
			Value:  "Execution skipped",
			Inline: false,
		})
	}

	return &discordWebhookPayload{
		Username:  discordUsername,
		AvatarURL: discordAvatarURL,
		Embeds:    []discordEmbed{embed},
	}
}

type discordWebhookPayload struct {
	Username  string         `json:"username"`
	AvatarURL string         `json:"avatar_url"`
	Embeds    []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color,omitempty"`
	Fields      []discordField `json:"fields,omitempty"`
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}
