// Package cmd provides the command-line interface for the application.
package cmd

import (
	"context"
	"log/slog"

	"github.com/hibare/GoCommon/v2/pkg/os/exec"
	"github.com/hibare/stashly/internal/config"
	"github.com/hibare/stashly/internal/dumpster"
	"github.com/hibare/stashly/internal/notifiers"
	"github.com/hibare/stashly/internal/storage/s3"
)

func doBackup(ctx context.Context, cfg *config.Config) error {
	store := s3.NewS3Storage(cfg)
	if err := store.Init(ctx); err != nil {
		return err
	}

	exec := exec.NewExec()
	dump := dumpster.NewDumpster(cfg, store, exec)
	notify := notifiers.NewNotifier(cfg)
	err := notify.InitStore()
	if err != nil {
		return err
	}

	// Add new backup
	dumpResp, err := dump.CreateDump(ctx)
	if err != nil {
		if nErr := notify.NotifyBackupFailure(ctx, err); nErr != nil {
			slog.ErrorContext(ctx, "Failed to send NotifyBackupFailure", "error", nErr)
		}
		return err
	}

	databases := dumpResp.ExportedDatabases
	key := dumpResp.StorageKey

	if nErr := notify.NotifyBackupSuccess(ctx, databases, key); nErr != nil {
		slog.ErrorContext(ctx, "Failed to send NotifyBackupSuccess", "error", nErr)
	}

	// Purge old backups
	if pErr := dump.PurgeDumps(ctx); pErr != nil {
		if nErr := notify.NotifyBackupDeleteFailure(ctx, pErr); nErr != nil {
			slog.ErrorContext(ctx, "Failed to send NotifyBackupDeleteFailure", "error", nErr)
		}
		return pErr
	}
	return nil
}
