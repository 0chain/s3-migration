//go:build integration
// +build integration

package dStorage

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/0chain/gosdk/core/conf"
	"github.com/0chain/gosdk/core/zcncrypto"
	"github.com/0chain/gosdk/zboxcore/blockchain"
	"github.com/0chain/gosdk/zboxcore/sdk"
	"github.com/0chain/gosdk/zboxcore/zboxutil"
	"github.com/0chain/s3migration/util"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func TestDStorageService_Upload(t *testing.T) {
	initDStorageSDK()
	ds, err := GetDStorageService("", "", "", "", false, 0)

	log.Println(ds.GetAvailableSpace(), ds.GetTotalSpace())

	localFilePath := ""
	remotePath := ""
	f, err := os.Open(localFilePath)
	if err != nil {
		log.Println(err)
	}

	defer f.Close()

	fileInfo, err := f.Stat()
	mimeType, err := zboxutil.GetFileContentType(f)
	if err != nil {
		log.Println(err)
	}

	err = ds.Upload(context.Background(), remotePath, f, fileInfo.Size(), mimeType, true)
	if err != nil {
		log.Println(err)
	}

	log.Println("upload complete")
}

func TestDStorageService_IsFileExist(t *testing.T) {
	initDStorageSDK()
	ds, err := GetDStorageService("", "", "", "", false, 0)
	if err != nil {
		log.Println(err)
	}

	remotePath := ""
	exists, err := ds.IsFileExist(context.Background(), remotePath)
	if err != nil {
		log.Println(err)
	}
	log.Println(remotePath, "file exists", exists)
}

func initDStorageSDK() {
	cfg, err := conf.LoadConfigFile(filepath.Join(util.GetDefaultConfigDir(), "config.yaml"))
	if err != nil {
		panic(err)
	}

	network, err := conf.LoadNetworkFile(filepath.Join(util.GetDefaultConfigDir(), "network.yaml"))
	if err != nil {
		// panic(err)
		fmt.Println(err)
	}

	var walletFilePath string

	walletFilePath = util.GetDefaultConfigDir() + string(os.PathSeparator) + "wallet.json"

	if _, err := os.Stat(walletFilePath); os.IsNotExist(err) {
		fmt.Println("ZCN wallet not defined in configurations")
		os.Exit(1)
	}

	f, err := os.Open(walletFilePath)
	if err != nil {
		fmt.Println("Error opening the wallet", err)
		os.Exit(1)
	}
	clientBytes, err := ioutil.ReadAll(f)
	if err != nil {
		fmt.Println("Error reading the wallet", err)
		os.Exit(1)
	}
	clientConfig := string(clientBytes)

	clientWallet := &zcncrypto.Wallet{}
	//minerjson, _ := json.Marshal(miners)
	//sharderjson, _ := json.Marshal(sharders)
	err = json.Unmarshal([]byte(clientConfig), clientWallet)
	if err != nil {
		fmt.Println("Invalid wallet at path:" + walletFilePath)
		os.Exit(1)
	}

	if err := sdk.InitStorageSDK(clientConfig, cfg.BlockWorker, cfg.ChainID, cfg.SignatureScheme, cfg.PreferredBlobbers); err != nil {
		panic(err)
	}

	// additional settings depending network latency
	blockchain.SetMaxTxnQuery(cfg.MaxTxnQuery)
	blockchain.SetQuerySleepTime(cfg.QuerySleepTime)

	conf.InitClientConfig(&cfg)

	if network.IsValid() {
		sdk.SetNetwork(network.Miners, network.Sharders)
	}

	sdk.SetNumBlockDownloads(10)
}
