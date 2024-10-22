package migration

import "time"

type MigrationConfig struct {
	AllocationID    string
	Skip            int
	Concurrency     int
	Bucket          string
	Region          string
	Prefix          string
	MigrateToPath   string
	DuplicateSuffix string
	Encrypt         bool
	RetryCount      int
	NewerThan       *time.Time
	OlderThan       *time.Time
	DeleteSource    bool
	StartAfter      string
	StateFilePath   string
	WorkDir         string
	ChunkSize       int64
	ChunkNumber     int
	BatchSize       int

	Source      string // "s3" (default) or "google_drive" or "dropbox"
	AccessToken string // if Source == "google_drive" or "dropbox"
}
