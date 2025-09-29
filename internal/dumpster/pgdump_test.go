package dumpster

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/hibare/GoCommon/v2/pkg/os/exec"
	"github.com/hibare/stashly/internal/config"
	"github.com/hibare/stashly/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewDumpster(t *testing.T) {
	cfg := &config.Config{}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	assert.NotNil(t, dumpster)
	assert.Equal(t, cfg, dumpster.cfg)
	assert.Equal(t, mockStore, dumpster.store)
	assert.Equal(t, mockExec, dumpster.exec)
	assert.Contains(t, dumpster.backupLocation, "export")
}

func TestDumpster_getEnvVars(t *testing.T) {
	cfg := &config.Config{
		Postgres: config.PostgresConfig{
			User:     "testuser",
			Password: "testpass",
			Host:     "localhost",
			Port:     "5432",
		},
	}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)
	envVars := dumpster.getEnvVars()

	expected := []string{
		"PGUSER=testuser",
		"PGPASSWORD=testpass",
		"PGHOST=localhost",
		"PGPORT=5432",
	}

	assert.Equal(t, expected, envVars)
}

func TestDumpster_runPreChecks_Success(t *testing.T) {
	cfg := &config.Config{}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock successful binary lookups
	mockExec.On("LookPath", "psql").Return("/usr/bin/psql", nil)
	mockExec.On("LookPath", "pg_dump").Return("/usr/bin/pg_dump", nil)

	err := dumpster.runPreChecks()

	require.NoError(t, err)
	mockExec.AssertExpectations(t)

	// Cleanup
	_ = os.RemoveAll(dumpster.backupLocation)
}

func TestDumpster_runPreChecks_BinaryNotFound(t *testing.T) {
	cfg := &config.Config{}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock failed binary lookup
	mockExec.On("LookPath", "psql").Return("", errors.New("binary not found"))

	err := dumpster.runPreChecks()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "psql not found in PATH")
	mockExec.AssertExpectations(t)
}

func TestDumpster_CreateDump_Success(t *testing.T) {
	cfg := &config.Config{
		Backup: config.BackupConfig{
			Encrypt: false,
		},
	}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)
	mockCmd := exec.NewMockCmdIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock successful pre-checks
	mockExec.On("LookPath", "psql").Return("/usr/bin/psql", nil)
	mockExec.On("LookPath", "pg_dump").Return("/usr/bin/pg_dump", nil)

	// Mock successful database listing
	mockExec.On("Command", mock.Anything, "psql", mock.Anything).Return(mockCmd)
	mockCmd.On("WithEnv", mock.Anything).Return(mockCmd)
	mockCmd.On("WithDir", dumpster.backupLocation).Return(mockCmd)
	mockCmd.On("WithStderr", os.Stderr).Return(mockCmd)
	mockCmd.On("Output").Return([]byte("db1\n"), nil)

	// Mock successful pg_dump
	mockExec.On("Command", mock.Anything, "pg_dump", mock.Anything).Return(mockCmd)
	mockCmd.On("WithEnv", mock.Anything).Return(mockCmd)
	mockCmd.On("WithDir", dumpster.backupLocation).Return(mockCmd)
	mockCmd.On("WithStderr", os.Stderr).Return(mockCmd)
	mockCmd.On("CombinedOutput").Return([]byte(""), nil)

	// Mock successful storage upload
	mockStore.On("Name").Return("test-storage")
	mockStore.On("Upload", mock.Anything).Return("backup-2024-01-01.tar.gz", nil)

	resp, err := dumpster.CreateDump(context.Background())

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 1, resp.TotalDatabases)
	assert.Equal(t, 1, resp.ExportedDatabases)
	assert.Equal(t, dumpster.backupLocation, resp.DumpLocation)
	assert.Equal(t, "backup-2024-01-01.tar.gz", resp.StorageKey)

	mockExec.AssertExpectations(t)
	mockCmd.AssertExpectations(t)
	mockStore.AssertExpectations(t)

	// Cleanup
	_ = os.RemoveAll(dumpster.backupLocation)
}

func TestDumpster_CreateDump_NoDatabasesExported(t *testing.T) {
	cfg := &config.Config{}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)
	mockCmd := exec.NewMockCmdIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock successful pre-checks
	mockExec.On("LookPath", "psql").Return("/usr/bin/psql", nil)
	mockExec.On("LookPath", "pg_dump").Return("/usr/bin/pg_dump", nil)

	// Mock successful database listing but no databases
	mockExec.On("Command", mock.Anything, "psql", mock.Anything).Return(mockCmd)
	mockCmd.On("WithEnv", mock.Anything).Return(mockCmd)
	mockCmd.On("WithDir", dumpster.backupLocation).Return(mockCmd)
	mockCmd.On("WithStderr", os.Stderr).Return(mockCmd)
	mockCmd.On("Output").Return([]byte(""), nil)

	resp, err := dumpster.CreateDump(context.Background())

	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "no databases were exported")

	mockExec.AssertExpectations(t)
	mockCmd.AssertExpectations(t)
}

func TestDumpster_CreateDump_PgDumpError(t *testing.T) {
	cfg := &config.Config{
		Backup: config.BackupConfig{
			Encrypt: false,
		},
	}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)
	mockCmd := exec.NewMockCmdIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock successful pre-checks
	mockExec.On("LookPath", "psql").Return("/usr/bin/psql", nil)
	mockExec.On("LookPath", "pg_dump").Return("/usr/bin/pg_dump", nil)

	// Mock successful database listing
	mockExec.On("Command", mock.Anything, "psql", mock.Anything).Return(mockCmd)
	mockCmd.On("WithEnv", mock.Anything).Return(mockCmd)
	mockCmd.On("WithDir", dumpster.backupLocation).Return(mockCmd)
	mockCmd.On("WithStderr", os.Stderr).Return(mockCmd)
	mockCmd.On("Output").Return([]byte("db1\n"), nil)

	// Mock failed pg_dump
	mockExec.On("Command", mock.Anything, "pg_dump", mock.Anything).Return(mockCmd)
	mockCmd.On("WithEnv", mock.Anything).Return(mockCmd)
	mockCmd.On("WithDir", dumpster.backupLocation).Return(mockCmd)
	mockCmd.On("WithStderr", os.Stderr).Return(mockCmd)
	mockCmd.On("CombinedOutput").Return([]byte("permission denied"), errors.New("access denied"))

	resp, err := dumpster.CreateDump(context.Background())

	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "no databases were exported")

	mockExec.AssertExpectations(t)
	mockCmd.AssertExpectations(t)
}

func TestDumpster_ListDumps_Success(t *testing.T) {
	cfg := &config.Config{}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock successful storage listing
	keys := []string{"backup-2024-01-01.tar.gz", "backup-2024-01-02.tar.gz"}
	mockStore.On("List").Return(keys, nil)
	mockStore.On("TrimPrefix", keys).Return(keys)

	dumps, err := dumpster.ListDumps(context.Background())

	require.NoError(t, err)
	// Note: The actual result will be transformed by datetime.SortDateTimes
	// So we just check that we get some result
	assert.NotEmpty(t, dumps)

	mockStore.AssertExpectations(t)
}

func TestDumpster_ListDumps_Empty(t *testing.T) {
	cfg := &config.Config{}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock empty storage listing
	mockStore.On("List").Return([]string{}, nil)

	dumps, err := dumpster.ListDumps(context.Background())

	require.NoError(t, err)
	assert.Empty(t, dumps)

	mockStore.AssertExpectations(t)
}

func TestDumpster_ListDumps_StorageError(t *testing.T) {
	cfg := &config.Config{}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock storage error
	mockStore.On("List").Return(nil, errors.New("storage connection failed"))

	dumps, err := dumpster.ListDumps(context.Background())

	require.Error(t, err)
	require.Nil(t, dumps)
	assert.Contains(t, err.Error(), "storage connection failed")

	mockStore.AssertExpectations(t)
}

func TestDumpster_PurgeDumps_Success(t *testing.T) {
	cfg := &config.Config{
		Backup: config.BackupConfig{
			RetentionCount: 2,
		},
	}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock successful storage listing
	keys := []string{"backup-2024-01-01.tar.gz", "backup-2024-01-02.tar.gz", "backup-2024-01-03.tar.gz"}
	mockStore.On("List").Return(keys, nil)
	mockStore.On("TrimPrefix", keys).Return(keys)

	// Mock successful deletion of old backup
	// Note: The actual key will be transformed by datetime.SortDateTimes
	mockStore.On("Delete", mock.Anything).Return(nil)

	err := dumpster.PurgeDumps(context.Background())

	require.NoError(t, err)

	mockStore.AssertExpectations(t)
}

func TestDumpster_PurgeDumps_NoDeletionNeeded(t *testing.T) {
	cfg := &config.Config{
		Backup: config.BackupConfig{
			RetentionCount: 3,
		},
	}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock storage listing with fewer keys than retention count
	keys := []string{"backup-2024-01-01.tar.gz", "backup-2024-01-02.tar.gz"}
	mockStore.On("List").Return(keys, nil)
	mockStore.On("TrimPrefix", keys).Return(keys)

	err := dumpster.PurgeDumps(context.Background())

	require.NoError(t, err)

	mockStore.AssertExpectations(t)
}

func TestDumpster_PurgeDumps_DeleteError(t *testing.T) {
	cfg := &config.Config{
		Backup: config.BackupConfig{
			RetentionCount: 2,
		},
	}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock successful storage listing
	keys := []string{"backup-2024-01-01.tar.gz", "backup-2024-01-02.tar.gz", "backup-2024-01-03.tar.gz"}
	mockStore.On("List").Return(keys, nil)
	mockStore.On("TrimPrefix", keys).Return(keys)

	// Mock failed deletion
	// Note: The actual key will be transformed by datetime.SortDateTimes
	mockStore.On("Delete", mock.Anything).Return(errors.New("delete failed"))

	err := dumpster.PurgeDumps(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "error deleting backup")

	mockStore.AssertExpectations(t)
}

func TestDumpster_Dump_Success(t *testing.T) {
	cfg := &config.Config{
		Backup: config.BackupConfig{
			Encrypt: false,
		},
	}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)
	mockCmd := exec.NewMockCmdIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock successful pre-checks
	mockExec.On("LookPath", "psql").Return("/usr/bin/psql", nil)
	mockExec.On("LookPath", "pg_dump").Return("/usr/bin/pg_dump", nil)

	// Mock successful database listing
	mockExec.On("Command", mock.Anything, "psql", mock.Anything).Return(mockCmd)
	mockCmd.On("WithEnv", mock.Anything).Return(mockCmd)
	mockCmd.On("WithDir", dumpster.backupLocation).Return(mockCmd)
	mockCmd.On("WithStderr", os.Stderr).Return(mockCmd)
	mockCmd.On("Output").Return([]byte("db1\n"), nil)

	// Mock successful pg_dump
	mockExec.On("Command", mock.Anything, "pg_dump", mock.Anything).Return(mockCmd)
	mockCmd.On("WithEnv", mock.Anything).Return(mockCmd)
	mockCmd.On("WithDir", dumpster.backupLocation).Return(mockCmd)
	mockCmd.On("WithStderr", os.Stderr).Return(mockCmd)
	mockCmd.On("CombinedOutput").Return([]byte(""), nil)

	// Mock successful storage upload
	mockStore.On("Name").Return("test-storage")
	mockStore.On("Upload", mock.Anything).Return("backup-2024-01-01.tar.gz", nil)

	// Mock successful purge
	keys := []string{"backup-2024-01-01.tar.gz"}
	mockStore.On("List").Return(keys, nil)
	mockStore.On("TrimPrefix", keys).Return(keys)
	mockStore.On("Delete", mock.Anything).Return(nil)

	resp, err := dumpster.Dump(context.Background())

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 1, resp.TotalDatabases)
	assert.Equal(t, 1, resp.ExportedDatabases)

	mockExec.AssertExpectations(t)
	mockCmd.AssertExpectations(t)
	mockStore.AssertExpectations(t)

	// Cleanup
	_ = os.RemoveAll(dumpster.backupLocation)
}

func TestDumpster_Dump_CreateDumpError(t *testing.T) {
	cfg := &config.Config{}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock failed pre-checks
	mockExec.On("LookPath", "psql").Return("", errors.New("binary not found"))

	resp, err := dumpster.Dump(context.Background())

	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "psql not found in PATH")

	mockExec.AssertExpectations(t)
}

func TestDumpster_Dump_PurgeError(t *testing.T) {
	cfg := &config.Config{
		Backup: config.BackupConfig{
			Encrypt: false,
		},
	}
	mockStore := storage.NewMockStorageIface(t)
	mockExec := exec.NewMockExecIface(t)
	mockCmd := exec.NewMockCmdIface(t)

	dumpster := NewDumpster(cfg, mockStore, mockExec)

	// Mock successful pre-checks
	mockExec.On("LookPath", "psql").Return("/usr/bin/psql", nil)
	mockExec.On("LookPath", "pg_dump").Return("/usr/bin/pg_dump", nil)

	// Mock successful database listing
	mockExec.On("Command", mock.Anything, "psql", mock.Anything).Return(mockCmd)
	mockCmd.On("WithEnv", mock.Anything).Return(mockCmd)
	mockCmd.On("WithDir", dumpster.backupLocation).Return(mockCmd)
	mockCmd.On("WithStderr", os.Stderr).Return(mockCmd)
	mockCmd.On("Output").Return([]byte("db1\n"), nil)

	// Mock successful pg_dump
	mockExec.On("Command", mock.Anything, "pg_dump", mock.Anything).Return(mockCmd)
	mockCmd.On("WithEnv", mock.Anything).Return(mockCmd)
	mockCmd.On("WithDir", dumpster.backupLocation).Return(mockCmd)
	mockCmd.On("CombinedOutput").Return([]byte(""), nil)

	// Mock successful storage upload
	mockStore.On("Name").Return("test-storage")
	mockStore.On("Upload", mock.Anything).Return("backup-2024-01-01.tar.gz", nil)

	// Mock failed purge
	mockStore.On("List").Return(nil, errors.New("storage error"))

	resp, err := dumpster.Dump(context.Background())

	require.Error(t, err)
	require.Nil(t, resp)
	assert.Contains(t, err.Error(), "storage error")

	mockExec.AssertExpectations(t)
	mockCmd.AssertExpectations(t)
	mockStore.AssertExpectations(t)

	// Cleanup
	_ = os.RemoveAll(dumpster.backupLocation)
}
