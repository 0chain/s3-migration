package util

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	zlogger "github.com/0chain/s3migration/logger"

	"github.com/spf13/viper"
)

const (
	ZGoSDKTimeFormat = "2006-01-02T15:04:05.999999Z"
)

// GetConfigDir get config directory , default is ~/.zcn/
func GetDefaultConfigDir() string {
	configDir := filepath.Join(GetHomeDir(), ".zcn")
	if err := os.MkdirAll(configDir, 0744); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	return configDir
}

// GetHomeDir Find home directory.
func GetHomeDir() string {
	idr, err := os.UserHomeDir()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	return idr
}

func GetAllocationIDFromFile(allocPath string) (allocationId string, err error) {
	var b []byte
	b, err = os.ReadFile(allocPath)
	if err != nil {
		return
	}

	return strings.ReplaceAll(strings.ReplaceAll(string(b), "\n", ""), " ", ""), nil
}

func SetAwsEnvCredentials(accessKey, secretKey string) (err error) {
	err = os.Setenv("AWS_ACCESS_KEY_ID", accessKey)
	if err != nil {
		return
	}

	return os.Setenv("AWS_SECRET_ACCESS_KEY", secretKey)
}

func GetAwsCredentialsFromFile(credPath string) (accessKey, secretKey string) {
	f, err := os.Open(credPath)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(f); err != nil {
		panic(err)
	}

	accessKey = v.GetString("aws_access_key")
	secretKey = v.GetString("aws_secret_key")
	return
}

func GetBucketRegionPrefixFromFile(credPath string) (bucket, region, prefix string, err error) {
	f, err := os.Open(credPath)
	if err != nil {
		return
	}
	defer f.Close()

	v := viper.New()
	v.SetConfigType("yaml")
	if err = v.ReadConfig(f); err != nil {
		return
	}

	bucket = v.GetString("bucket")
	region = v.GetString("region")
	prefix = v.GetString("prefix")
	return
}

func GetAllocationIDFromEnv() string {
	return os.Getenv("ALLOCATION_ID")
}

func GetAwsCredentialsFromEnv() (string, string) {
	return os.Getenv("AWS_ACCESS_KEY"), os.Getenv("AWS_SECRET_KEY")
}

func ConvertGoSDKTimeToTime(in string) time.Time {
	t, err := time.Parse(ZGoSDKTimeFormat, in)
	if err != nil {
		log.Println("failed to parse time string from gosdk")
		return time.Now().UTC()
	}

	return t
}

func Retry(attempts int, sleep time.Duration, f func() error) (err error) {
	for i := 0; i < attempts; i++ {
		if err = f(); err != nil {
			zlogger.Logger.Error("retrying after error:", err)
			time.Sleep(sleep)
			continue
		}
		return
	}
	return fmt.Errorf("after %d attempts, last error: %s", attempts, err)
}

// signalTrap traps the registered signals and notifies the caller.
func SignalTrap(sig ...os.Signal) <-chan struct{} {
	// channel to notify the caller.
	trapCh := make(chan struct{}, 1)

	go func(chan<- struct{}) {
		// channel to receive signals.
		sigCh := make(chan os.Signal, 1)
		defer close(sigCh)

		// `signal.Notify` registers the given channel to
		// receive notifications of the specified signals.
		signal.Notify(sigCh, sig...)

		// Wait for the signal.
		<-sigCh
		// Once signal has been received stop signal Notify handler.
		signal.Stop(sigCh)

		// Notify the caller.
		trapCh <- struct{}{}
	}(trapCh)

	return trapCh
}
