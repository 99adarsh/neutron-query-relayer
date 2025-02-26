package relay

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	tmtypes "github.com/tendermint/tendermint/types"

	neutronmetrics "github.com/neutron-org/neutron-query-relayer/internal/metrics"

	"github.com/cosmos/relayer/v2/relayer"

	"github.com/neutron-org/neutron-query-relayer/internal/config"
	neutrontypes "github.com/neutron-org/neutron/x/interchainqueries/types"

	"go.uber.org/zap"
)

// TxHeight describes tendermint filter by tx.height that we use to get only actual txs
const TxHeight = "tx.height"

// Relayer is controller for the whole app:
// 1. takes events from Neutron chain
// 2. dispatches each query by type to fetch proof for the right query
// 3. submits proof for a query back to the Neutron chain
type Relayer struct {
	cfg         config.NeutronQueryRelayerConfig
	txQuerier   TXQuerier
	logger      *zap.Logger
	storage     Storage
	txProcessor TXProcessor
	kvProcessor KVProcessor
	targetChain *relayer.Chain
}

func NewRelayer(
	cfg config.NeutronQueryRelayerConfig,
	txQuerier TXQuerier,
	store Storage,
	txProcessor TXProcessor,
	kvProcessor KVProcessor,
	targetChain *relayer.Chain,
	logger *zap.Logger,
) *Relayer {
	return &Relayer{
		cfg:         cfg,
		txQuerier:   txQuerier,
		logger:      logger,
		storage:     store,
		txProcessor: txProcessor,
		kvProcessor: kvProcessor,
		targetChain: targetChain,
	}
}

// Run starts the relaying process: subscribes on the incoming interchain query messages from the
// Neutron and performs the queries by interacting with the target chain and submitting them to
// the Neutron chain.
func (r *Relayer) Run(
	ctx context.Context,
	queriesTasksQueue <-chan neutrontypes.RegisteredQuery, // Input tasks come from this channel
	submittedTxsTasksQueue chan PendingSubmittedTxInfo, // Tasks for the TxSubmitChecker are sent to this channel
) error {
	for {
		var err error
		select {
		case query := <-queriesTasksQueue:
			start := time.Now()
			neutronmetrics.SetSubscriberTaskQueueNumElements(len(queriesTasksQueue))
			switch query.QueryType {
			case string(neutrontypes.InterchainQueryTypeKV):
				msg := &MessageKV{QueryId: query.Id, KVKeys: query.Keys}
				err = r.processMessageKV(ctx, msg)
			case string(neutrontypes.InterchainQueryTypeTX):
				msg := &MessageTX{QueryId: query.Id, TransactionsFilter: query.TransactionsFilter}
				err = r.processMessageTX(ctx, msg, submittedTxsTasksQueue)

				var critErr ErrSubmitTxProofCritical
				if errors.As(errors.Unwrap(err), &critErr) {
					return err
				}
			default:
				err = fmt.Errorf("unknown query type: %s", query.QueryType)
			}

			if err != nil {
				r.logger.Error("could not process message", zap.Uint64("query_id", query.Id), zap.Error(err))
				neutronmetrics.AddFailedRequest(string(query.QueryType), time.Since(start).Seconds())
			} else {
				neutronmetrics.AddSuccessRequest(string(query.QueryType), time.Since(start).Seconds())
			}
		case <-ctx.Done():
			r.logger.Info("context cancelled, shutting down relayer...")
			return nil
		}
	}
}

// processMessageKV handles an incoming KV interchain query message and passes it to the kvProcessor for further processing.
func (r *Relayer) processMessageKV(ctx context.Context, m *MessageKV) error {
	r.logger.Debug("running processMessageKV for msg", zap.Uint64("query_id", m.QueryId))
	return r.kvProcessor.ProcessAndSubmit(ctx, m)
}

// processMessageTX handles an incoming TX interchain query message. It fetches proven transactions
// from the target chain using the message transactions filter value, and submits the result to the
// Neutron chain.
func (r *Relayer) processMessageTX(ctx context.Context, m *MessageTX, submittedTxsTasksQueue chan PendingSubmittedTxInfo) error {
	r.logger.Debug("running processMessageTX for msg", zap.Uint64("query_id", m.QueryId))
	queryString, err := r.buildTxQuery(ctx, m)
	if err != nil {
		return fmt.Errorf("failed to build tx query string: %w", err)
	}
	r.logger.Debug("tx query to search transactions",
		zap.Uint64("query_id", m.QueryId),
		zap.String("query", queryString))

	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	txs, errs := r.txQuerier.SearchTransactions(cancelCtx, queryString)
	lastProcessedHeight := uint64(0)
	for tx := range txs {
		if tx.Height > lastProcessedHeight && lastProcessedHeight > 0 {
			err := r.storage.SetLastQueryHeight(m.QueryId, lastProcessedHeight)
			if err != nil {
				return fmt.Errorf("failed to save last height of query: %w", err)
			}
			r.logger.Debug("block completely processed",
				zap.Uint64("query_id", m.QueryId),
				zap.Uint64("processed_height", lastProcessedHeight),
				zap.Uint64("next_height_to_process", tx.Height))
		}
		lastProcessedHeight = tx.Height

		hash := hex.EncodeToString(tmtypes.Tx(tx.Tx.Data).Hash())
		txExists, err := r.storage.TxExists(m.QueryId, hash)
		if err != nil {
			return fmt.Errorf("failed to check tx existence: %w", err)
		}

		if txExists {
			r.logger.Debug("transaction already submitted",
				zap.Uint64("query_id", m.QueryId),
				zap.String("hash", hash),
				zap.Uint64("height", tx.Height))
			continue
		}

		err = r.txProcessor.ProcessAndSubmit(ctx, m.QueryId, tx, submittedTxsTasksQueue)
		if err != nil {
			return fmt.Errorf("failed to process txs: %w", err)
		}
	}

	stoppedWithErr := <-errs
	if stoppedWithErr != nil {
		return fmt.Errorf("failed to query txs: %w", stoppedWithErr)
	}

	if lastProcessedHeight > 0 {
		err = r.storage.SetLastQueryHeight(m.QueryId, lastProcessedHeight)
		if err != nil {
			return fmt.Errorf("failed to save last height of query: %w", err)
		}
		r.logger.Debug("the final block completely processed",
			zap.Uint64("query_id", m.QueryId),
			zap.Uint64("processed_height", lastProcessedHeight))
	} else {
		r.logger.Debug("no results found for the query", zap.Uint64("query_id", m.QueryId))
	}

	return nil
}

func (r *Relayer) buildTxQuery(ctx context.Context, m *MessageTX) (string, error) {
	queryLastHeight, err := r.getLastQueryHeight(ctx, m.QueryId)
	if err != nil {
		return "", fmt.Errorf("could not get last query height: %w", err)
	}

	var params neutrontypes.TransactionsFilter
	if err = json.Unmarshal([]byte(m.TransactionsFilter), &params); err != nil {
		return "", fmt.Errorf("could not unmarshal transactions filter: %w", err)
	}
	// add filter by tx.height (tx.height>n)
	params = append(params, neutrontypes.TransactionsFilterItem{Field: TxHeight, Op: "gt", Value: queryLastHeight})

	queryString, err := queryFromTxFilter(params)
	if err != nil {
		return "", fmt.Errorf("failed to process tx query params: %w", err)
	}

	return queryString, nil
}

// getLastQueryHeight returns last query height & no err if query exists in storage, also initializes query with height = 0  if not exists yet
func (r *Relayer) getLastQueryHeight(ctx context.Context, queryID uint64) (uint64, error) {
	height, found, err := r.storage.GetLastQueryHeight(queryID)
	if err != nil {
		return 0, fmt.Errorf("could not get last query height from storage: %w", err)
	}
	if !found {
		height = uint64(0)

		if r.cfg.InitialTxSearchOffset != 0 {
			latestHeight, err := r.targetChain.ChainProvider.QueryLatestHeight(ctx)
			if err != nil {
				return 0, fmt.Errorf("could not get latest target chain height: %w", err)
			}
			if uint64(latestHeight) > r.cfg.InitialTxSearchOffset {
				height = uint64(latestHeight) - r.cfg.InitialTxSearchOffset
			}
			r.logger.Debug("set initial height", zap.Uint64("query_id", queryID), zap.Uint64("initial_height", height), zap.Uint64("offset", r.cfg.InitialTxSearchOffset))
		}

		err = r.storage.SetLastQueryHeight(queryID, height)
		if err != nil {
			return 0, fmt.Errorf("failed to set a 0 last height for an uninitialised query: %w", err)
		}
	}

	return height, nil
}

// QueryFromTxFilter creates query from transactions filter like
// `key1{=,>,>=,<,<=}value1 AND key2{=,>,>=,<,<=}value2 AND ...`
func queryFromTxFilter(params neutrontypes.TransactionsFilter) (string, error) {
	queryParamsList := make([]string, 0, len(params))
	for _, row := range params {
		sign, err := getOpSign(row.Op)
		if err != nil {
			return "", err
		}
		switch r := row.Value.(type) {
		case string:
			queryParamsList = append(queryParamsList, fmt.Sprintf("%s%s'%s'", row.Field, sign, r))
		case float64:
			queryParamsList = append(queryParamsList, fmt.Sprintf("%s%s%d", row.Field, sign, uint64(r)))
		case uint64:
			queryParamsList = append(queryParamsList, fmt.Sprintf("%s%s%d", row.Field, sign, r))
		}
	}
	return strings.Join(queryParamsList, " AND "), nil
}

func getOpSign(op string) (string, error) {
	switch strings.ToLower(op) {
	case "eq":
		return "=", nil
	case "gt":
		return ">", nil
	case "gte":
		return ">=", nil
	case "lt":
		return "<", nil
	case "lte":
		return "<=", nil
	default:
		return "", fmt.Errorf("unsupported operator %s", op)
	}
}
