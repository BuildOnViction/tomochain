## Install

### Golang

[https://go.dev/dl/](https://go.dev/dl/)

Golang MacOS - amd64

Set GOROOT và GOPATH (arcording to computer)

```bash
curl -o golang.pkg https://dl.google.com/go/go1.20.4.darwin-amd64.pkg
sudo open golang.pkg
```

```bash
export GOROOT=$HOME/usr/local/go
export GOPATH=$HOME/go
```

### Tomo

Create tomo folder

```bash
mkdir tomo
cd tomo
```

Install Tomo and library

```bash
git clone https://github.com/tomochain/tomochain/
cd tomochain
go mod tidy -e
make all
cd ..
```

Alias for tomo:

```bash
alias tomo=$PWD/tomochain/build/bin/tomo
alias puppeth=$PWD/tomochain/build/bin/puppeth
alias bootnode=$PWD/tomochain/build/bin/bootnode
```

## Setup node and account

- Create nodes file
  ```bash
  mkdir nodes
  cd nodes
  mkdir 1
  cd ..
  ```
- Create account or import account (with your private key) (at least 2 account)

  - Create Keystore directory:
    `mkdir keystore`

    - Create a password directory:
      Example: `mkdir $HOME/pw`

    `export PASSWORD_DIRECTORY=[DIRECTORY TO STORE PASSWORD OF KEYSTORE FILE]`

  - Create new account:
    ```bash
    touch $PASSWORD_DIRECTORY/pw.txt
    echo [YOUR_PASSWORD] >> $PASSWORD_DIRECTORY/pw.txt
    ```
    ```bash
    tomo account new \
          --password $PASSWORD_DIRECTORY/pw.txt \
          --keystore $PWD/keystore/1
    ```
  - Import account:

    - Create a private key directory:
      Example: `mkdir $HOME/pk`

    `export PRIVATE_KEY_DIRECTORY=[DIRECTORY TO STORE PRIVATE KEY]`

    ```bash
    touch $PRIVATE_KEY_DIRECTORY/pk.txt
    echo [YOUR_PRIVATE_KEY] >> $PRIVATE_KEY_DIRECTORY/pk.txt
    ```

    ```bash
    tomo  account import $PRIVATE_KEY_DIRECTORY/pk.txt \
        --keystore $PWD/keystore/1 \
        --password $PRIVATE_KEY_DIRECTORY/pk1txt
    ```

## Create genesis file with `puppeth`

- Run `puppeth`
  ```bash
  puppeth
  ```
  - Set chain name: `c98chain`
  - Configure new genesis: `2`
  - Select `POSV` consensus: `3`
  - Set blocktime (default 2 seconds): `Enter`
  - Set reward of each epoch: `250`
  - Set addresses to be initial masternodes: Account address created before
  - Set account to seal: Account 1
  - Set number of blocks of each epoch (default 900): `Enter`
  - Set gap: `5`
  - Set foundation wallet address: `Enter`
  - Account confirm Foundation MultiSignWallet: Account address created before
  - Require for confirm tx in Foudation MultiSignWallet: `1`
  - Account confirm Team MultiSignWallet: Account address created before
  - Require for confirm tx in Team MultiSignWallet: `1`
  - Enter swap wallet address for fund 55 million TOMO: Account address created before
  - Enter account be pre-funded:
    ```bash
    1BE6F1C0BAc392980262b084306751FD34Ab4462
    32911b48d723F04c92B8fda38CBa6dC1D2B4d058
    951564eD947442dF0088df5a52CC8F665520a45f
    1BE6F1C0BAc392980262b084306751FD34Ab4462
    756A6142dd54dD0b19cC6589Cffd81b23E67171b
    c980E9513D8983cA507F4A44946036ea069239d1
    ```
  - Enter network ID: `3172`
- Export genesis file
  - Select `2. Manage existing genesis`
  - Select `2. Export genesis configuration`
  - Enter genesis filename: `genesis.json`
- `Ctrl + C` to end

## Init node with genesis file

```bash
tomo --datadir nodes/1 init genesis.json
```

## Setup bootnode

- Init bootnode key
  ```bash
  bootnode -genkey bootnode.key
  ```
- Start bootnode

  ```bash
  bootnode -nodekey ./bootnode.key
  ```

  Get bootnode info

  `BOOTNODE_INFO` example: `"enode://372853cfc9cc509bdd79db961cf791e8b2c8fdbadd5b4a25b0e59187f3be9a6e1d26e381f8ed4ae71d81c72ad7f53430af605955293df66660232ad235633880@[::]:30301"`

## Run node

- Open new terminal
  `export PASSWORD_DIRECTORY=[DIRECTORY TO STORE PASSWORD OF KEYSTORE FILE]`

  `alias tomo=$PWD/tomochain/build/bin/tomo`

`YOUR_ACCOUNT_ADDRESS` example: `"0x79d3620f9379d043eaea262f1cac689fc906d5a1"`

- Node 1

  ```bash
  tomo --syncmode "full" \
  --datadir nodes/1 --networkid 3172 --port 10303 \
  --keystore keystore/1 --password $PASSWORD_DIRECTORY/pw.txt \
  --rpc --rpccorsdomain "*" --rpcaddr 0.0.0.0 --rpcport 1545 --rpcvhosts "*" \
  --rpcapi "admin,db,eth,net,web3,personal,debug" \
  --gcmode "archive" \
  --ws --wsaddr 0.0.0.0 --wsport 1546 --wsorigins "*" --unlock [YOUR_ACCOUNT_ADDRESS] \
  --identity "NODE1" \
  --mine --gasprice 2500 \ --bootnodesv5 [BOOTNODE_INFO] \
  console
  ```

- Node 1 can Commit and seal block

## Connect node to sync and execute

- Open IPC node 1
  ```bash
  tomo attach nodes/1/tomo.ipc
  ```

→ Successful create node.
