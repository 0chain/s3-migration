package migration

import "time"

type MigrationConfig struct {
	AllocationID    string
	Skip            int
	Bucket          string
	Region          string
	Prefix          string
	MigrateToPath   string
	DuplicateSuffix string
	WhoPays         int
	Encrypt         bool
	RetryCount      int
	NewerThan       *time.Time
	OlderThan       *time.Time
	DeleteSource    bool
	StartAfter      string
	StateFilePath   string
	WorkDir         string
}
