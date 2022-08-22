# Description
Interchain query relayer implementation for Cosmos

Makes interchain queries possible.
For example there is blockchain N that needs to make query to blockchain T.
N -> T

Blockchain N submits an interchain query with needed params and so on.

Relayer sees the incoming event from blockchain N and:
1. Tries to parse it from list of supported queries
2. If successful, gets proofs for all the needed data for query
3. If successful, submits transaction with proofs back to blockchain N

Blockchain L can then verify the result for the query.

# Running in development
- export environment you need (e.g. `export $(grep -v '^#' .env.example | xargs)` note: change rpc addresses to actual)
- `$ make dev`

For more configuration parameters see struct in internal/config/config.go

# TODO: add link to integration tests. Because we cannot do it now (cmd line from neutron deleted ability to register query, now available only from contracts)
# Testing

## Run unit tests
`$ make test`

## Testing with 2 neutron-chains (easier for development)

### terminal 1
we expect that both this repo and neutron will be located in one dir
1. `git clone git@github.com:neutron-org/neutron.git`
2. `cd neutron`
3. `make build && make init && make start-rly`

### terminal 2
see test-2/config/genesis.json for $VAL2 value

1. Create delegation from demowallet2 to val2 on test-2 chain
```
VAL2=neutronvaloper1qnk2n4nlkpw9xfqntladh74w6ujtulwnqshepx
DEMOWALLET2=$(neutrond keys show demowallet2 -a --keyring-backend test --home ./data/test-2)
echo "DEMOWALLET2: $DEMOWALLET2"
./build/neutrond tx staking delegate $VAL2 1stake --from demowallet2 --keyring-backend test --home ./data/test-2 --chain-id=test-2 -y
```
2. Register interchain query
```
./build/neutrond tx interchainqueries register-interchain-query test-2 connection-0 x/staking/DelegatorDelegations '{"delegator": "neutron10h9stc5v6ntgeygf5xf945njqq5h32r54rf7kf"}' 1 --from demowallet1 --gas 10000000 --gas-adjustment 1.4 --gas-prices 0.5stake --broadcast-mode block --chain-id test-1 --keyring-backend test --home ./data/test-1 --node tcp://127.0.0.1:16657
```

### terminal 3
#### via cli
1. set env from env list via way you prefer (e.g. `export $(grep -v '^#' .env.example | xargs)` )
2. `make dev`


#### via Docker
currently `neutron` is a private repo, so you need to run `ssh-add ~/.ssh/id_rsa`
*note*: we're going to remove this after making all our repos public
1. Build docker image 
`make build-docker`
2. Run
`docker run --env-file .env.example -v $PWD/../neutron/data:/data -p 9999:9999 neutron-org/cosmos-query-relayer`
note: this command uses relative path to mount keys, run this from root path of `cosmos-query-relayer`
### Logging
We are using [zap.loger](https://github.com/uber-go/zap)
By default, project spawns classical Production logger. so if there is a need to customize it, consider editing envs (see .env.example for exapmles)


##  Environment Notes
### Common 

| Key                                                  | type                                                           | description                                                                                                                                                                                                                                                                                                                                                                                                                                 | optional |
|------------------------------------------------------|----------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|
| `RELAYER_NEUTRON_CHAIN_CHAIN_PREFIX`                 | `string`                                                       | chain prefix of neutron chain                                                                                                                                                                                                                                                                                                                                                                                                               | required |
| `RELAYER_NEUTRON_CHAIN_RPC_ADDR`                     | `string`                                                       | rpc address of neutron chain                                                                                                                                                                                                                                                                                                                                                                                                                | required |
| `RELAYER_NEUTRON_CHAIN_CHAIN_ID `                    | `string`                                                       | neutron chain id                                                                                                                                                                                                                                                                                                                                                                                                                            | required |
| `RELAYER_NEUTRON_CHAIN_HOME_DIR   `                  | `string`                                                       | path to keys directory                                                                                                                                                                                                                                                                                                                                                                                                                      | required |
| `RELAYER_NEUTRON_CHAIN_SIGN_KEY_NAME`                | `string`                                                       | key name                                                                                                                                                                                                                                                                                                                                                                                                                                    | required |
| `RELAYER_NEUTRON_CHAIN_TIMEOUT `                     | `time`                                                         | timeout of neutron chain provider                                                                                                                                                                                                                                                                                                                                                                                                           | required |
| `RELAYER_NEUTRON_CHAIN_GAS_PRICES`                   | `string`                                                       | specifies how much the user is willing to pay per unit of gas, which can be one or multiple denominations of token                                                                                                                                                                                                                                                                                                                          | required |
| `RELAYER_NEUTRON_CHAIN_GAS_LIMIT`                    | `string`                                                       | the maximum price a relayer user is willing to pay for relayer's paid blockchain actions                                                                                                                                                                                                                                                                                                                                                    | required |
| `RELAYER_NEUTRON_CHAIN_GAS_ADJUSTMENT`               | `float`                                                        | used to scale gas up in order to avoid underestimating. For example, users can specify their gas adjustment as 1.5 to use 1.5 times the estimated gas                                                                                                                                                                                                                                                                                       | required |
| `RELAYER_NEUTRON_CHAIN_TX_BROADCAST_TYPE`            | `BroadcastTxSync` OR `BroadcastTxAsync` OR `BroadcastTxCommit` | - `BroadcastTxCommit` broadcasts transaction bytes to a Tendermint node and waits for a commit. An error is only returned if there is no RPC node connection or if broadcasting fails. <br/>`BroadcastTxSync` broadcasts transaction bytes to a Tendermint node synchronously (i.e. returns after CheckTx execution).<br/>  `BroadcastTxAsync` broadcasts transaction bytes to a Tendermint node asynchronously (i.e. returns immediately). | required |
| `RELAYER_NEUTRON_CHAIN_CONNECTION_ID`                | `string`                                                       | neutron chain connection ID                                                                                                                                                                                                                                                                                                                                                                                                                 | required |
| `RELAYER_NEUTRON_CHAIN_CLIENT_ID `                   | `string`                                                       | IBC client ID for an IBC connection between Neutron chain and target chain (where the result was obtained from)                                                                                                                                                                                                                                                                                                                             | required |
| `RELAYER_NEUTRON_CHAIN_DEBUG `                       | `bool`                                                         | flag to run neutron chain provider in debug mode                                                                                                                                                                                                                                                                                                                                                                                            | required |
| `RELAYER_NEUTRON_CHAIN_ACCOUNT_PREFIX `              | `string`                                                       | neutron chain account prefix                                                                                                                                                                                                                                                                                                                                                                                                                | required |
| `RELAYER_NEUTRON_CHAIN_KEYRING_BACKEND`              | `string`                                                       | [see](https://docs.cosmos.network/master/run-node/keyring.html#the-kwallet-backend)                                                                                                                                                                                                                                                                                                                                                         | required |
| `RELAYER_NEUTRON_CHAIN_OUTPUT_FORMAT`                | `json`  OR `yaml`                                              | target chain provider output format                                                                                                                                                                                                                                                                                                                                                                                                         | required |
| `RELAYER_NEUTRON_CHAIN_SIGN_MODE_STR `               | `string`                                                       | [see](https://docs.cosmos.network/master/core/transactions.html#signing-transactions) also consider use short variation, e.g. `direct`                                                                                                                                                                                                                                                                                                      | required |
| `RELAYER_TARGET_CHAIN_RPC_ADDR`                      | `string`                                                       | rpc address of target chain                                                                                                                                                                                                                                                                                                                                                                                                                 | required |
| `RELAYER_TARGET_CHAIN_CHAIN_ID `                     | `string`                                                       | target chain id                                                                                                                                                                                                                                                                                                                                                                                                                             | required |
| `RELAYER_TARGET_CHAIN_ACCOUNT_PREFIX `               | `string`                                                       | target chain account prefix                                                                                                                                                                                                                                                                                                                                                                                                                 | required |
| `RELAYER_TARGET_CHAIN_VALIDATOR_ACCOUNT_PREFIX `     | `string`                                                       | target chain validator account prefix                                                                                                                                                                                                                                                                                                                                                                                                       | required |
| `RELAYER_TARGET_CHAIN_TIMEOUT `                      | `time`                                                         | timeout of target chain provider                                                                                                                                                                                                                                                                                                                                                                                                            | required |
| `RELAYER_TARGET_CHAIN_CONNECTION_ID`                 | `time`                                                         | target chain connetcion ID                                                                                                                                                                                                                                                                                                                                                                                                                  | required |
| `RELAYER_TARGET_CHAIN_CLIENT_ID `                    | `string`                                                       | IBC client ID for an IBC connection between Neutron chain and target chain (where the result was obtained from)                                                                                                                                                                                                                                                                                                                             | required |
| `RELAYER_TARGET_CHAIN_DEBUG `                        | `bool`                                                         | flag to run target chain provider in debug mode                                                                                                                                                                                                                                                                                                                                                                                             | required |
| `RELAYER_TARGET_CHAIN_OUTPUT_FORMAT`                 | `json`  or `yaml`                                              | target chain provider output format                                                                                                                                                                                                                                                                                                                                                                                                         | required |
| `RELAYER_REGISTRY_ADDRESSES`                         | `string`                                                       | a list of comma-separated smart-contract addresses for which the relayer processes interchain queries                                                                                                                                                                                                                                                                                                                                       | required |
| `RELAYER_ALLOW_TX_QUERIES`                           | `bool`                                                         | if true relayer will process tx queries  (if `false`,  relayer will drop them)                                                                                                                                                                                                                                                                                                                                                              | required |
| `RELAYER_ALLOW_KV_CALLBACKS`                         | `bool`                                                         | if `true`, will pass proofs as sudo callbacks to contracts                                                                                                                                                                                                                                                                                                                                                                                  | required |
| `RELAYER_MIN_KV_UPDATE_PERIOD`                       | `uint`                                                         | minimal period of queries execution and submission (not less than `n` blocks)                                                                                                                                                                                                                                                                                                                                                               | optional |

### Running via docker
-  with local chains use `host.docker.internal` in `RELAYER_NEUTRON_CHAIN_RPC_ADDR` and `RELAYER_TARGET_CHAIN_RPC_ADDR` instead of `localhost`/`127.0.0.1`
- Note that wallet data path is in the root of docker container `RELAYER_TARGET_CHAIN_HOME_DIR=/data/test-2` `RELAYER_NEUTRON_CHAIN_HOME_DIR=/data/test-1`
### Running without docker 
- consider to change  `RELAYER_NEUTRON_CHAIN_RPC_ADDR` & `RELAYER_TARGET_CHAIN_RPC_ADDR` to actual rpc addresses 
- `RELAYER_TARGET_CHAIN_HOME_DIR` `RELAYER_NEUTRON_CHAIN_HOME_DIR` also need to be changed (keys are generated in `terminal 1`)
