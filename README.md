# s3-migration - a CLI for migrating AWS s3 bucket to 0Chain dStorage 
This program helps to migrate files in s3 bucket to 0chain dStorage.

## Running s3-migration

When you run the `s3migration` command in terminal with no arguments, it will list all the available commands and the global flags.

**Command**|**Description**
:-----:|:-----:
[help](#help)|Help about any command
[migrate](#migrate)|Migrate user data from S3 bucket to dStorage

## Running Test cases

`go test ./...`

### Global Flags

Global Flags are parameters in s3mgrt that can be used with any command to override the default configuration supports the following global parameters.

| Flags                      | Description                                                  | Usage                                             |
| -------------------------- | ------------------------------------------------------------ | ------------------------------------------------- |
| --config string            | Specify configuration file (default is [$HOME/.zcn/config.yaml](#zcnconfigyaml)) | s3mgrt migrate --config config1.yaml              |
| --configDir string         | Specify a configuration directory (default is $HOME/.zcn) | s3migration migrate --configDir /$HOME/.zcn2           |
| -h, --help                 | Gives more information about a particular command.           | s3migration migrate --help                             |
| --network string           | Specify a network file to overwrite the network details(default is [$HOME/.zcn/network.yaml](#zcnnetworkyaml)) | s3migration migrate --network network1.yaml            |
| --wallet string            | Specify a wallet file or 2nd wallet (default is $HOME/.zcn/wallet.json) | s3migration migrate --wallet wallet2.json              |
| --wallet_client_id string  | Specify client id. If wallet key is also provided then wallet_private_key is required | s3migration migrate --wallet_client_id "wallet client id" |
| --wallet_client_key string | Specify a wallet client_key (By default client_key specified in $HOME/.zcn/wallet.json is used) | s3migration migrate --wallet_client_key  "client key" |
| --wallet_private_key string | Specify wallet private key| s3migration migrate --wallet_private_key "wallet private key"|

## Migrate Command

`migrate` command is used to migrate files from s3 buckets to some remote directory(default is /) by using aws-s3-sdk and 0chain gosdk. All the objects from bucket will be migrated.
However user can specify some prefix to migrate only the files with those prefix. Also if there is name conflict within dStorage file and bucket file use can
specify whether to skip, replace or duplicate them. Migration state is maintained in some file so user can also resume migration operation if some error had
occurred in previous migration session. User can also specify whether to delete migrated file. Note the defaults.

	Note: Addition of new object or modification of existing file while migrating is not recommended, as it cannot track such changes and you might lose your data.

| Flags          | Required | Description                                               | Default        | Valid Values |
|--------------------|----------|-----------------------------------------------------------|----------------|--------------|
| access-key | yes if aws-cred-path not provided | access-key of aws               |  | string    |
| secret-key | yes if aws-cred-path not provided | secret-key of aws               |  | string    |
| aws-cred-path | yes if access/ secret key not provided | File Path to aws credentials                |  | file path    |
| allocation | yes if alloc-path not provided | allocation ID for dStorage (required if alloc path is not provided)               |  | string    |
| alloc-path | yes if allocation not provided | File Path to allocation text                |  | file path    |
| bucket | yes | Bucket to migrate                |  | string    |
| concurrency |  | number of concurrent files to process concurrently during migration                | 10 | int    |
| delete-source |  | Delete object in s3 that is migrated to dStorage               | false | boolean    |
| dup-suffix |  | Duplicate suffix to use for migrated file               | _copy | string    |
| encrypt |  | pass this option to encrypt and upload the file               | false | boolean    |
| migrate-to |  | Remote path where buckets will be migrated to               | / | string    |
| owner-pays |  | Read payment source(Default: owner pays               | true | boolean    |
| newer-than |  | eg; 7d10h --> migrate objects that is newer than 7 days 10 hours               |  | string    |
| older-than |  | eg; 7d10h --> migrate objects that is newer than 7 days 10 hours               |  | string    |
| prefix |  | Migrate objects starting with this prefix               |  | string    |
| region |  | AWS S3 Bucket location               | us-east-2 | string    |
| resume |  | pass this option to resume migration from previous state               | false | boolean    |
| skip |  | 0 --> Replace existing files; 1 --> Skip migration; 2 --> Duplicate               | 1 | int    |
| wd |  | Working directory               | $HOME/.s3migration | string    |

## BenchMark

**Data Shard**|**Parity**|**Upload Size**|**File count**|**Time Taken**|**Network Speed**
:----------:|:---------:|:-----:|:-----:|:------:|:------:
4|2|20 gb|199|1 hr 25min|15 MBps








