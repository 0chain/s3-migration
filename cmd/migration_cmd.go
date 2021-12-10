package cmd

import (
	"context"
	"errors"
	"fmt"
	"github.com/0chain/s3migration/model"
	s3svc "github.com/0chain/s3migration/s3/service"
	"github.com/spf13/cobra"

	"github.com/0chain/gosdk/zboxcore/sdk"
	"github.com/0chain/s3migration/controller"
	"github.com/0chain/s3migration/util"
)

var (
	allocationId         string
	accessKey, secretKey string
	buckets              []string
	migrateToPath        string
	concurrency          int
	encrypt              bool
	resume               bool
	skip                 int // 0 --> Replace; 1 --> Skip; 2 --> Duplicate
	region               string
	allocationTextPath   string
	whoPays              string
	awsCredPath          string
)

// migrateCmd is the migrateFromS3 sub command to migrate whole objects from some buckets.
func init() {
	rootCmd.AddCommand(migrateCmd)

	//flags related to dStorage
	migrateCmd.PersistentFlags().StringVar(&allocationId, "allocation", "", "allocation ID for dStorage")
	migrateCmd.Flags().StringVar(&allocationTextPath, "alloc-path", "alloc-path", "File Path to allocation text")
	migrateCmd.Flags().StringVar(&whoPays, "attr-who-pays-for-reads", "", "Read payment source")
	migrateCmd.Flags().BoolVar(&encrypt, "encrypt", false, "pass this option to encrypt and upload the file")
	//flags related to s3
	migrateCmd.PersistentFlags().StringVar(&accessKey, "access-key", "", "access-key of aws")
	migrateCmd.PersistentFlags().StringVar(&secretKey, "secret-key", "", "secret-key of aws")
	migrateCmd.PersistentFlags().StringVar(&region, "region", "", "region of s3 buckets")
	migrateCmd.PersistentFlags().StringSliceVar(&buckets, "buckets", []string{}, "specific s3 buckets to use. Use bucketName:prefix format if prefix filter is required or only bucketName for migrating all objects")
	migrateCmd.Flags().StringVar(&migrateToPath, "migrate-to", "/", "Remote path where buckets will be migrated to")
	migrateCmd.Flags().StringVar(&awsCredPath, "aws-cred-path", "", "File Path to aws credentials")

	migrateCmd.Flags().IntVar(&concurrency, "concurrency", 10, "number of concurrent files to process concurrently during migration")
	migrateCmd.Flags().BoolVar(&resume, "resume", false, "pass this option to resume migration from previous state")
	migrateCmd.Flags().IntVar(&skip, "skip", 1, "0 --> Replace existing files; 1 --> Skip migration; 2 --> Duplicate")
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate user data from S3 bucket to dStorage",
	Long: `Migrate files from s3 buckets to some remote directory(default is /) by using aws-s3-sdk and 0chain gosdk. All the objects from bucket will be migrated.
	However user can specify some prefix to migrate only the files with those prefix. Also if there is name conflict within dStorage file and bucket file use can 
	specify whether to skip, replace or duplicate them. Migration state is maintained is some file so user can also resume migration operation if some error had 
	occurred in previous migration session. User can also specify whether to delete migrated file. Note the defaults.

	Note: Addition of new object or modification of existing file while migrating is not recommended, as it cannot track such changes and you might loose your data.
	`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Flags().Parse(args)
		var err error
		if allocationId == "" {
			allocationId = util.GetAllocationIDFromEnv()
			if allocationId == "" {
				if allocationTextPath == "" {
					return errors.New("allocation text file not passed in argument")
				}

				allocationId, err = util.GetAllocationIDFromFile(allocationTextPath)
				if err != nil {
					return err
				}
			}

			if allocationId == "" {
				return errors.New("allocation id is missing")
			}
		}

		if accessKey == "" || secretKey == "" {
			accessKey, secretKey = util.GetAwsCredentialsFromFile(awsCredPath)
		}

		if accessKey != "" && secretKey != "" {
			if err := util.SetAwsEnvCredentials(accessKey, secretKey); err != nil {
				return fmt.Errorf("failed to set aws custom credentials")
			}
		}

		if buckets == nil {
			buckets = util.GetBucketsFromFile(awsCredPath) // if buckets is nil, migrate all buckets in root dir
		}

		if skip < 0 || skip > 2 {
			return fmt.Errorf("skip value not in range 0-2. Provided value is %v", skip)
		}

		allocation, err := sdk.GetAllocation(allocationId)
		if err != nil {
			return err
		}

		s3Service, err := s3svc.NewService(region)
		if err != nil {
			return err
		}

		appConfig := model.AppConfig{
			Region:        region,
			Skip:          skip,
			Resume:        resume,
			Concurrency:   concurrency,
			Buckets:       buckets,
			MigrateToPath: migrateToPath,
			WhoPays:       whoPays,
			Encrypt:       encrypt,
		}

		migration := controller.NewMigration()

		if err := migration.InitMigration(context.Background(), allocation, s3Service, &appConfig); err != nil {
			return err
		}

		return migration.Migrate()
	},
}
