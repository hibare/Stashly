// Package notifiers implements various notification mechanisms for backup events.
package notifiers

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/hibare/stashly/internal/config"
	"github.com/hibare/stashly/internal/notifiers/discord"
)

var (
	// ErrNotifiersDisabled is returned when notifiers are globally disabled.
	ErrNotifiersDisabled = errors.New("notifiers are disabled")

	// ErrNotifierDisabled is returned when a specific notifier is disabled.
	ErrNotifierDisabled = errors.New("notifier is disabled")
)

// NotifiersIface defines the interface that all notifier implementations must satisfy.
// revive:disable-next-line exported
type NotifiersIface interface {
	Enabled() bool
	NotifyBackupSuccess(ctx context.Context, databases int, key string) error
	NotifyBackupFailure(ctx context.Context, err error) error
	NotifyBackupDeleteFailure(ctx context.Context, err error) error
}

// NotifierStoreIface defines the interface for managing multiple notifiers.
type NotifierStoreIface interface {
	Enabled() bool
	NotifyBackupSuccess(ctx context.Context, databases int, key string) error
	NotifyBackupFailure(ctx context.Context, err error) error
	NotifyBackupDeleteFailure(ctx context.Context, err error) error
	InitStore() error
}

// Notifier manages multiple notifier implementations.
type Notifier struct {
	cfg   *config.Config
	mu    sync.RWMutex
	store []NotifiersIface
}

func (n *Notifier) register(nf NotifiersIface) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.store = append(n.store, nf)
}

// Enabled checks if notifiers are globally enabled in the configuration.
func (n *Notifier) Enabled() bool {
	return n.cfg.Notifiers.Enabled
}

// NotifyBackupSuccess sends a backup success notification using all enabled notifiers.
func (n *Notifier) NotifyBackupSuccess(ctx context.Context, databases int, key string) error {
	if !n.Enabled() {
		return ErrNotifierDisabled
	}

	for _, notifier := range n.store {
		if !notifier.Enabled() {
			slog.DebugContext(ctx, "Notifier disabled; skipping NotifyBackupSuccess")
			continue
		}
		if err := notifier.NotifyBackupSuccess(ctx, databases, key); err != nil {
			slog.ErrorContext(ctx, "Failed to send NotifyBackupSuccess", "error", err)
		}
	}

	return nil
}

// NotifyBackupFailure sends a backup failure notification using all enabled notifiers.
func (n *Notifier) NotifyBackupFailure(ctx context.Context, nErr error) error {
	if !n.Enabled() {
		return ErrNotifierDisabled
	}

	for _, notifier := range n.store {
		if !notifier.Enabled() {
			slog.DebugContext(ctx, "Notifier disabled; skipping NotifyBackupFailure")
			continue
		}
		if err := notifier.NotifyBackupFailure(ctx, nErr); err != nil {
			slog.ErrorContext(ctx, "Failed to send NotifyBackupFailure", "error", err)
		}
	}

	return nil
}

// NotifyBackupDeleteFailure sends a backup deletion failure notification using all enabled notifiers.
func (n *Notifier) NotifyBackupDeleteFailure(ctx context.Context, nErr error) error {
	if !n.Enabled() {
		return ErrNotifierDisabled
	}

	for _, notifier := range n.store {
		if !notifier.Enabled() {
			slog.DebugContext(ctx, "Notifier disabled; skipping NotifyBackupDeleteFailure")
			continue
		}
		if err := notifier.NotifyBackupDeleteFailure(ctx, nErr); err != nil {
			slog.ErrorContext(ctx, "Failed to send NotifyBackupDeleteFailure", "error", err)
		}
	}

	return nil
}

// InitStore initializes and registers all available notifiers.
func (n *Notifier) InitStore() error {
	d, err := discord.NewDiscordNotifier(n.cfg)
	if err != nil {
		return err
	}

	n.register(d)

	return nil
}

// NewNotifier creates a new Notifier instance with the provided configuration.
func NewNotifier(cfg *config.Config) NotifierStoreIface {
	return &Notifier{cfg: cfg}
}
