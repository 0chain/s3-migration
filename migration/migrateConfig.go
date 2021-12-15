package migration

import "time"

type MigrationConfig struct {
	AllocationID  string
	Skip          int
	Concurrency   int
	Bucket        string
	Region        string
	Prefix        string
	MigrateToPath string
	WhoPays       int
	Encrypt       bool
	RetryCount    int
	NewerThan     *time.Time
	OlderThan     *time.Time
	DeleteSource  bool
	StartAfter    string
}
