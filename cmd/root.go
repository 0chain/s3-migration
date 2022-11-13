package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/0chain/gosdk/core/conf"
	"github.com/0chain/gosdk/core/logger"
	"github.com/0chain/s3migration/util"

	"github.com/spf13/cobra"

	"github.com/0chain/gosdk/zboxcore/blockchain"

	"github.com/0chain/gosdk/core/zcncrypto"

	"github.com/0chain/gosdk/zboxcore/sdk"
	"github.com/0chain/gosdk/zcncore"
	zlogger "github.com/0chain/s3migration/logger"
)

var (
	cfgFile          string
	networkFile      string
	walletFile       string
	walletClientID   string
	walletClientKey  string
	walletPrivateKey string
	configDir        string
	nonce            int64
	bSilent          bool

	rootCmd = &cobra.Command{
		Use:   "s3migration",
		Short: "S3-Migration to migrate s3 buckets to dStorage allocation",
		Long: `S3-Migration uses 0chain-gosdk to communicate with 0chain network. It uses AWS SDK for Go program
		to communicate with s3.`,
	}

	// clientWallet zcncrypto.Wallet
)
var clientConfig string

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yaml", "config file")
	rootCmd.PersistentFlags().StringVar(&networkFile, "network", "network.yaml", "network file to overwrite the network details")
	rootCmd.PersistentFlags().StringVar(&walletFile, "wallet", "wallet.json", "wallet file")
	rootCmd.PersistentFlags().StringVar(&walletClientID, "wallet_client_id", "", "wallet client_id")
	rootCmd.PersistentFlags().StringVar(&walletClientKey, "wallet_client_key", "", "wallet client_key")
	rootCmd.PersistentFlags().StringVar(&walletPrivateKey, "wallet_private_key", "", "wallet private key")
	rootCmd.PersistentFlags().Int64Var(&nonce, "withNonce", 0, "nonce that will be used in transaction (default is 0)")

	rootCmd.PersistentFlags().StringVar(&configDir, "configDir", util.GetDefaultConfigDir(), "configuration directory")
	rootCmd.PersistentFlags().BoolVar(&bSilent, "silent", false, "Do not show interactive sdk logs (shown by default)")
}

var VersionStr string

func Execute() error {
	rootCmd.Version = VersionStr
	return rootCmd.Execute()
}

func initConfig() {
	cfg, err := conf.LoadConfigFile(filepath.Join(configDir, cfgFile))
	if err != nil {
		panic(err)
	}

	network, err := conf.LoadNetworkFile(filepath.Join(configDir, networkFile))
	if err != nil {
		// panic(err)
		fmt.Println(err)
	}
	// syncing loggers
	logger.SyncLoggers([]*logger.Logger{zcncore.GetLogger(), sdk.GetLogger()})

	// set the log file
	zcncore.SetLogFile("cmdlog.log", !bSilent)
	sdk.SetLogFile("cmdlog.log", !bSilent)
	zlogger.SetLogFile("s3migration.log", !bSilent)

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

	clientWallet := &zcncrypto.Wallet{}
	if walletClientID != "" && walletClientKey != "" {
		if walletPrivateKey == "" {
			fmt.Println("Empty private key passed")
			os.Exit(1)
		}
		clientWallet.ClientID = walletClientID
		clientWallet.ClientKey = walletClientKey
		keys := zcncrypto.KeyPair{
			PublicKey:  walletClientKey,
			PrivateKey: walletPrivateKey,
		}
		clientWallet.Keys = append(clientWallet.Keys, keys)

		var clientBytes []byte
		clientBytes, err = json.Marshal(clientWallet)
		clientConfig = string(clientBytes)
		if err != nil {
			fmt.Println("Invalid wallet data passed:" + walletClientID + " " + walletClientKey)
			os.Exit(1)
		}
	} else {
		var walletFilePath string
		if walletFile != "" {
			if filepath.IsAbs(walletFile) {
				walletFilePath = walletFile
			} else {
				walletFilePath = configDir + string(os.PathSeparator) + walletFile
			}
		} else {
			walletFilePath = configDir + string(os.PathSeparator) + "wallet.json"
		}

		if _, err = os.Stat(walletFilePath); os.IsNotExist(err) {
			fmt.Println("ZCN wallet not defined in configurations")
			os.Exit(1)
		}

		clientBytes, err := os.ReadFile(walletFilePath)
		if err != nil {
			fmt.Println("Error reading the wallet", err)
			os.Exit(1)
		}
		clientConfig = string(clientBytes)

		//minerjson, _ := json.Marshal(miners)
		//sharderjson, _ := json.Marshal(sharders)
		err = json.Unmarshal([]byte(clientConfig), clientWallet)
		if err != nil {
			fmt.Println("Invalid wallet at path:" + walletFilePath)
			os.Exit(1)
		}
	}

	//init the storage sdk with the known miners, sharders and client wallet info
	if err := sdk.InitStorageSDK(clientConfig, cfg.BlockWorker, cfg.ChainID, cfg.SignatureScheme, cfg.PreferredBlobbers, nonce); err != nil {
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
