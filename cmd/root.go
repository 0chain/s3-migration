package cmd

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/0chain/gosdk/core/conf"
	"github.com/0chain/gosdk/core/logger"
	"github.com/0chain/s3migration/util"

	"github.com/spf13/cobra"

	"github.com/0chain/gosdk/zboxcore/blockchain"

	"github.com/0chain/gosdk/core/zcncrypto"

	"github.com/0chain/gosdk/zboxcore/sdk"
	"github.com/0chain/gosdk/zcncore"
)

var (
	cfgFile, networkFile, walletFile, walletClientID, walletClientKey, configDir string
	bSilent                                                                      bool

	rootCmd = &cobra.Command{
		Use:   "s3mgrt",
		Short: "S3-Migration to migrate s3 buckets to dStorage allocation",
		Long: `S3-Migration uses 0chain-gosdk to communicate with 0chain network. It uses AWS SDK for Go program
		to communicate with s3.`,
	}

	clientWallet *zcncrypto.Wallet
)

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yaml", "config file")
	rootCmd.PersistentFlags().StringVar(&networkFile, "network", "network.yaml", "network file to overwrite the network details")
	rootCmd.PersistentFlags().StringVar(&walletFile, "wallet", "wallet.json", "wallet file")
	rootCmd.PersistentFlags().StringVar(&walletClientID, "wallet_client_id", "", "wallet client_id")
	rootCmd.PersistentFlags().StringVar(&walletClientKey, "wallet_client_key", "", "wallet client_key")
	rootCmd.PersistentFlags().StringVar(&configDir, "configDir", util.GetConfigDir(), "configuration directory")
	rootCmd.PersistentFlags().BoolVar(&bSilent, "silent", false, "Do not show interactive sdk logs (shown by default)")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}

func initConfig() {
	cfg, err := conf.LoadConfigFile(filepath.Join(configDir, cfgFile))
	if err != nil {
		panic(err)
	}

	network, _ := conf.LoadNetworkFile(filepath.Join(configDir, networkFile))

	// syncing loggers
	logger.SyncLoggers([]*logger.Logger{zcncore.GetLogger(), sdk.GetLogger()})

	// set the log file
	zcncore.SetLogFile("cmdlog.log", !bSilent)
	sdk.SetLogFile("cmdlog.log", !bSilent)

	if network.IsValid() {
		zcncore.SetNetwork(network.Miners, network.Sharders)
		conf.InitChainNetwork(&conf.Network{
			Miners:   network.Miners,
			Sharders: network.Sharders,
		})
	}

	err = zcncore.InitZCNSDK(cfg.BlockWorker, cfg.SignatureScheme,
		zcncore.WithChainID(cfg.ChainID),
		zcncore.WithMinSubmit(cfg.MinSubmit),
		zcncore.WithMinConfirmation(cfg.MinConfirmation),
		zcncore.WithConfirmationChainLength(cfg.ConfirmationChainLength))

	if err != nil {
		panic(err)
	}

	var walletBytes []byte
	if len(walletClientID) > 0 && len(walletClientKey) > 0 {
		clientWallet.ClientID = walletClientID
		clientWallet.ClientKey = walletClientKey

		walletBytes, err = json.Marshal(clientWallet)

		if err != nil {
			fmt.Println("Invalid wallet data passed:" + walletClientID + " " + walletClientKey)
			panic(err)
		}
	} else {
		var walletFilePath string
		if len(walletFile) > 0 {
			if filepath.IsAbs(walletFile) {
				walletFilePath = walletFile
			} else {
				walletFilePath = filepath.Join(configDir, walletFile)
			}
		} else {
			walletFilePath = filepath.Join(configDir, "wallet.json")
		}

		walletBytes, err := ioutil.ReadFile(walletFilePath)

		if err != nil {
			panic(err)
		}

		if err := json.Unmarshal(walletBytes, clientWallet); err != nil {
			panic(err)
		}
	}

	//init the storage sdk with the known miners, sharders and client wallet info
	if err := sdk.InitStorageSDK(string(walletBytes), cfg.BlockWorker, cfg.ChainID, cfg.SignatureScheme, cfg.PreferredBlobbers); err != nil {
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
