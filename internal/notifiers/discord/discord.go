// Package discord provides implementations for various notification services.
package discord

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hibare/GoCommon/v2/pkg/notifiers/discord"
	"github.com/hibare/stashly/internal/config"
	"github.com/hibare/stashly/internal/constants"
)

const (
	successColor         = 1498748
	failureColor         = 14554702
	deletionFailureColor = 14590998
)

// Discord sends notifications to a Discord channel via webhook.
type Discord struct {
	Cfg    *config.Config
	client discord.ClientIface
}

// Enabled checks if the Discord notifier is enabled in the configuration.
func (d *Discord) Enabled() bool {
	return d.Cfg.Notifiers.Discord.Enabled
}

// NotifyBackupSuccess sends a success notification to the Discord channel.
func (d *Discord) NotifyBackupSuccess(ctx context.Context, databases int, key string) error {
	message := discord.Message{
		Embeds: []discord.Embed{
			{
				Color: successColor,
				Fields: []discord.EmbedField{
					{
						Name:   "Key",
						Value:  key,
						Inline: false,
					},
					{
						Name:   "Databases",
						Value:  strconv.Itoa(databases),
						Inline: false,
					},
				},
			},
		},
		Components: []discord.Component{},
		Username:   constants.ProgramIdentifier,
		Content:    fmt.Sprintf("**PG-DB Backup Successful** - *%s*", d.Cfg.App.InstanceID),
	}

	return d.client.Send(ctx, &message)
}

// NotifyBackupFailure sends a failure notification to the Discord channel.
func (d *Discord) NotifyBackupFailure(ctx context.Context, err error) error {
	message := discord.Message{
		Embeds: []discord.Embed{
			{
				Title:       "Error",
				Description: err.Error(),
				Color:       failureColor,
			},
		},
		Components: []discord.Component{},
		Username:   constants.ProgramIdentifier,
		Content:    fmt.Sprintf("**PG-DB Backup Failed** - *%s*", d.Cfg.App.InstanceID),
	}

	return d.client.Send(ctx, &message)
}

// NotifyBackupDeleteFailure sends a deletion failure notification to the Discord channel.
func (d *Discord) NotifyBackupDeleteFailure(ctx context.Context, err error) error {
	message := discord.Message{
		Embeds: []discord.Embed{
			{
				Title:       "Error",
				Description: err.Error(),
				Color:       deletionFailureColor,
			},
		},
		Components: []discord.Component{},
		Username:   constants.ProgramIdentifier,
		Content:    fmt.Sprintf("**PG-DB Backup Deletion Failed** - *%s*", d.Cfg.App.InstanceID),
	}

	return d.client.Send(ctx, &message)
}

// NewDiscordNotifier creates a new Discord notifier instance.
func NewDiscordNotifier(cfg *config.Config) (*Discord, error) {
	client, err := discord.NewClient(discord.Options{
		WebhookURL: cfg.Notifiers.Discord.Webhook,
	})
	if err != nil {
		return nil, err
	}

	return &Discord{
		Cfg:    cfg,
		client: client,
	}, nil
}
