package util

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
)

// GetConfigDir get config directory , default is ~/.zcn/
func GetConfigDir() string {

	configDir := GetHomeDir() + string(os.PathSeparator) + ".zcn"

	if err := os.MkdirAll(configDir, 0744); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	return configDir
}

// GetHomeDir Find home directory.
func GetHomeDir() string {
	// Find home directory.
	idr, err := homedir.Dir()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	return idr
}

func GetAllocationIDFromFile(allocPath string) (allocationId string, err error) {
	var b []byte
	b, err = ioutil.ReadFile(allocPath)
	if err != nil {
		return
	}

	//return strings.TrimSpace(string(b))

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
	v := viper.New()

	v.AddConfigPath(credPath)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		return
	}

	accessKey = v.GetString("aws_access_key_id")
	secretKey = v.GetString("aws_secret_access_key")

	return
}

func GetBucketsFromFile(credPath string) (buckets []string) {
	v := viper.New()

	v.AddConfigPath(credPath)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return nil
	}

	buckets = v.GetStringSlice("buckets")

	return
}

func GetAllocationIDFromEnv() string {
	return os.Getenv("ALLOC")
}

// readLines reads a whole file into memory
// and returns a slice of its lines.
func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func TrimSuffixPrefix(in, sp string) string {
	for {
		if strings.HasPrefix(in, sp) {
			in = strings.TrimPrefix(in, sp)
			continue
		}

		break
	}

	for {
		if strings.HasSuffix(in, sp) {
			in = strings.TrimSuffix(in, sp)
			continue
		}

		break
	}

	return in
}
