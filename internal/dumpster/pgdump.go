// Package dumpster provides functionality to create, list, and purge PostgreSQL database dumps.
package dumpster

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/hibare/GoCommon/v2/pkg/crypto/gpg"
	"github.com/hibare/GoCommon/v2/pkg/datetime"
	"github.com/hibare/GoCommon/v2/pkg/file"
	"github.com/hibare/GoCommon/v2/pkg/os/exec"
	"github.com/hibare/stashly/internal/config"
	"github.com/hibare/stashly/internal/constants"
	"github.com/hibare/stashly/internal/storage"
)

// DumpsterIface defines the interface for dumpster operations.
// revive:disable-next-line exported
type DumpsterIface interface {
	Dump(ctx context.Context) (int, string, error)
	ListDumps(ctx context.Context) ([]string, error)
	PurgeDumps(ctx context.Context) error
}

// Dumpster handles PostgreSQL database dumps and interactions with storage backends.
type Dumpster struct {
	store          storage.StorageIface
	cfg            *config.Config
	exec           exec.ExecIface
	backupLocation string
	gpg            gpg.GPGIface
}

func (d *Dumpster) getEnvVars() []string {
	return []string{
		fmt.Sprintf("PGUSER=%s", d.cfg.Postgres.User),
		fmt.Sprintf("PGPASSWORD=%s", d.cfg.Postgres.Password),
		fmt.Sprintf("PGHOST=%s", d.cfg.Postgres.Host),
		fmt.Sprintf("PGPORT=%s", d.cfg.Postgres.Port),
	}
}

func (d *Dumpster) runPreChecks() error {
	// Remove old backup location if exists
	if err := os.RemoveAll(d.backupLocation); err != nil {
		return err
	}

	// Create backup location
	if err := os.MkdirAll(d.backupLocation, 0750); err != nil {
		return err
	}

	// Check if required binaries are available
	binaries := []string{"psql", "pg_dump"}

	for _, bin := range binaries {
		if _, err := d.exec.LookPath(bin); err != nil {
			return fmt.Errorf("%s not found in PATH: %w", bin, err)
		}
	}
	return nil
}

type exportResponse struct {
	totalDatabases    int
	exportedDatabases int
	exportLocation    string
}

func (d *Dumpster) export(ctx context.Context) (*exportResponse, error) {
	totalDatabases := 0
	exportedDatabases := 0
	databases := []string{}

	envVars := d.getEnvVars()

	// Get list of non-template databases using psql machine output
	query := "SELECT datname FROM pg_database WHERE datistemplate = false AND datname NOT IN ('postgres','defaultdb');"

	output, err := d.exec.Command(ctx, "psql", "-At", "-c", query).
		WithEnv(envVars).
		WithDir(d.backupLocation).
		WithStderr(os.Stderr).
		Output()

	if err != nil {
		return nil, fmt.Errorf("error getting list of databases: %w", err)
	}

	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		databases = append(databases, line)
		totalDatabases++
	}

	slog.DebugContext(ctx, "Databases to be dumped", "databases", databases, "location", d.backupLocation)

	for _, db := range databases {
		slog.InfoContext(ctx, "Processing database", "database", db)

		outFile := filepath.Join(d.backupLocation, db+".sql")
		out, cErr := d.exec.Command(ctx, "pg_dump", "--no-owner", "--no-acl", "--dbname="+db, "--file="+outFile).
			WithEnv(envVars).
			WithDir(d.backupLocation).
			CombinedOutput()
		if cErr != nil {
			slog.WarnContext(ctx, "Error dumping database", "database", db, "error", cErr, "output", string(out))
			continue
		}
		exportedDatabases++
		slog.InfoContext(ctx, "Successfully dumped database", "database", db)
	}

	return &exportResponse{
		totalDatabases:    totalDatabases,
		exportedDatabases: exportedDatabases,
		exportLocation:    d.backupLocation,
	}, nil
}

// DumpResponse holds information about the dump operation.
type DumpResponse struct {
	TotalDatabases    int
	ExportedDatabases int
	DumpLocation      string
	ArchiveLocation   string
	StorageKey        string
}

// CreateDump creates a PostgreSQL dump, optionally encrypts it, uploads it to storage, and returns details.
func (d *Dumpster) CreateDump(ctx context.Context) (*DumpResponse, error) {
	if err := d.runPreChecks(); err != nil {
		return nil, err
	}

	resp, err := d.export(ctx)
	if err != nil {
		return nil, err
	}

	dumpResp := &DumpResponse{
		TotalDatabases:    resp.totalDatabases,
		ExportedDatabases: resp.exportedDatabases,
		DumpLocation:      resp.exportLocation,
	}

	if resp.exportedDatabases <= 0 {
		return nil, errors.New("no databases were exported")
	}

	archiveResp, err := file.ArchiveDir(resp.exportLocation, nil)
	if err != nil {
		return nil, err
	}

	archivePath := archiveResp.ArchivePath

	uploadFilePath := archivePath

	if d.cfg.Backup.Encrypt {
		slog.DebugContext(ctx, "fetching gpg key", "key_id", d.cfg.Encryption.GPG.KeyID, "key_server", d.cfg.Encryption.GPG.KeyServer)
		_, gErr := d.gpg.FetchGPGPubKeyFromKeyServer(d.cfg.Encryption.GPG.KeyID, d.cfg.Encryption.GPG.KeyServer)
		if gErr != nil {
			slog.WarnContext(ctx, "Error downloading gpg key", "error", gErr)
			return nil, gErr
		}

		slog.DebugContext(ctx, "Encrypting archive file", "file", archivePath)
		encryptedFilePath, gErr := d.gpg.EncryptFile(archivePath)
		if gErr != nil {
			slog.WarnContext(ctx, "Error encrypting archive file", "error", gErr)
			return nil, gErr
		}
		slog.DebugContext(ctx, "Encrypted file", "file", encryptedFilePath)
		uploadFilePath = encryptedFilePath
	}

	slog.InfoContext(ctx, "Uploading backup", "file", uploadFilePath, "storage", d.store.Name())
	key, err := d.store.Upload(ctx, uploadFilePath)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "Backup uploaded", "location", key)
	dumpResp.ArchiveLocation = archivePath
	dumpResp.StorageKey = key
	return dumpResp, nil
}

// ListDumps lists available dumps in the storage backend, sorted by date.
func (d *Dumpster) ListDumps(ctx context.Context) ([]string, error) {
	keys, err := d.store.List(ctx)
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		slog.InfoContext(ctx, "No backups found")
		return []string{}, nil
	}

	keys = d.store.TrimPrefix(keys)
	keys = datetime.SortDateTimes(keys)
	slog.DebugContext(ctx, "Found backups", "keys", keys)
	return keys, nil
}

// PurgeDumps deletes old dumps from storage based on the retention policy.
func (d *Dumpster) PurgeDumps(ctx context.Context) error {
	keys, err := d.ListDumps(ctx)
	if err != nil {
		return err
	}

	if len(keys) <= d.cfg.Backup.RetentionCount {
		slog.InfoContext(ctx, "No backups to delete")
		return nil
	}

	keysToDelete := keys[d.cfg.Backup.RetentionCount:]
	slog.InfoContext(ctx, "Found backups to delete", "count", len(keysToDelete), "retention", d.cfg.Backup.RetentionCount)

	for _, key := range keysToDelete {
		slog.InfoContext(ctx, "Deleting backup", "key", key)
		if sErr := d.store.Delete(ctx, key); sErr != nil {
			slog.ErrorContext(ctx, "Error deleting backup", "key", key, "error", sErr)
			return fmt.Errorf("error deleting backup %s: %w", key, sErr)
		}
	}
	slog.InfoContext(ctx, "Deletion completed successfully")
	return nil
}

// Dump creates a dump and purges old dumps based on retention policy.
func (d *Dumpster) Dump(ctx context.Context) (*DumpResponse, error) {
	resp, err := d.CreateDump(ctx)
	if err != nil {
		return nil, err
	}

	if pErr := d.PurgeDumps(ctx); pErr != nil {
		return nil, pErr
	}
	return resp, nil
}

// NewDumpster creates a new Dumpster instance with the provided configuration, storage backend, and executor.
func NewDumpster(cfg *config.Config, store storage.StorageIface, exec exec.ExecIface) *Dumpster {
	return &Dumpster{
		store:          store,
		cfg:            cfg,
		exec:           exec,
		backupLocation: filepath.Join(os.TempDir(), constants.ExportDir),
		gpg:            gpg.NewGPG(gpg.Options{}),
	}
}
