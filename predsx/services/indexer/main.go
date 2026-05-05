package main

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/predsx/predsx/libs/config"
	kafkaclient "github.com/predsx/predsx/libs/kafka-client"
	"github.com/predsx/predsx/libs/schemas"
	"github.com/predsx/predsx/libs/service"
)

// Polymarket CTF Contract on Polygon
const CTFContractAddress = "0x4D97DCd97eC945f40cF65F87097ACe5EA0476045"

func main() {
	svc := service.NewBaseService("indexer")

	svc.Run(context.Background(), func(ctx context.Context) error {
		kafkaBrokers := config.GetEnv("KAFKA_BROKERS", "localhost:9092")
		outputTopic := config.GetEnv("ONCHAIN_TRADES_TOPIC", "predsx.trades.onchain")
		rpcURL := config.GetEnv("POLYGON_RPC_URL", "")

		if rpcURL == "" {
			svc.Logger.Warn("no POLYGON_RPC_URL provided, indexer idle")
			<-ctx.Done()
			return nil
		}

		kafkaclient.EnsureTopics(ctx, []string{kafkaBrokers}, map[string]int{
			outputTopic: 6,
		}, svc.Logger)

		producer := kafkaclient.NewTypedProducer[schemas.OnChainTradeEvent]([]string{kafkaBrokers}, outputTopic, svc.Logger)
		defer producer.Close()

		client, err := ethclient.DialContext(ctx, rpcURL)
		if err != nil {
			svc.Logger.Error("failed to connect to ethereum node", "error", err)
			return err
		}
		defer client.Close()

		contractAddress := common.HexToAddress(CTFContractAddress)
		query := ethereum.FilterQuery{
			Addresses: []common.Address{contractAddress},
		}

		logs := make(chan types.Log)
		sub, err := client.SubscribeFilterLogs(ctx, query, logs)
		if err != nil {
			svc.Logger.Error("failed to subscribe to logs", "error", err)
			return err
		}

		svc.Logger.Info("listening to polygon blocks for Polymarket trades", "contract", CTFContractAddress)

		// ERC1155 TransferSingle signature
		transferSingleSig := crypto.Keccak256Hash([]byte("TransferSingle(address,address,address,uint256,uint256)"))

		for {
			select {
			case <-ctx.Done():
				return nil
			case err := <-sub.Err():
				svc.Logger.Error("subscription error", "error", err)
				time.Sleep(5 * time.Second)
			case vLog := <-logs:
				if len(vLog.Topics) > 0 && vLog.Topics[0] == transferSingleSig {
					if len(vLog.Topics) < 4 {
						continue // Malformed event
					}
					
					from := common.HexToAddress(vLog.Topics[2].Hex()).Hex()
					to := common.HexToAddress(vLog.Topics[3].Hex()).Hex()

					if len(vLog.Data) >= 64 {
						tokenID := new(big.Int).SetBytes(vLog.Data[:32]).String()
						value := new(big.Int).SetBytes(vLog.Data[32:]).String()

						evt := schemas.OnChainTradeEvent{
							TxHash:    vLog.TxHash.Hex(),
							Maker:     from,
							Taker:     to,
							TokenID:   tokenID,
							Amount:    value,
							Timestamp: time.Now(),
						}

						_ = producer.Publish(ctx, evt.TxHash, evt)
					}
				}
			}
		}
	})
}
