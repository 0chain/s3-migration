package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	thrown "github.com/0chain/errors"
	"github.com/0chain/gosdk/core/sys"
	"github.com/spf13/viper"
)

type DropboxConfig struct {
	accessToken string
}
var (ErrMssingConfig = errors.New("[conf]missing config file")
		ErrBadParsing = errors.New("parsing error")
		ErrInvalidToken=errors.New("invalid access Token"))

func loadDropboxFile(file string) (DropboxConfig, error) {
	var cfg DropboxConfig
	var err error

	_, err = sys.Files.Stat(file)

	if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return cfg, thrown.Throw(ErrMssingConfig, file)
			}
			return cfg, err
		}

	v := viper.New()

	v.SetConfigFile(file)

	if err := v.ReadInConfig(); err != nil {
		return cfg, thrown.Throw(ErrBadParsing, err.Error())
	}

	return LoadConfig(v)

}


func LoadConfig(v *viper.Viper) (DropboxConfig, error) {
	var cfg DropboxConfig

	accessToken := strings.TrimSpace(v.GetString("access_token"))

	if len(accessToken) == 0 {
		fmt.Printf("accessToken  + %v\n", accessToken)
		return cfg, thrown.Throw(ErrInvalidToken, "token is empty")
	}


	return cfg, nil
}