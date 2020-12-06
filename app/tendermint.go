package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	state2 "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/store"

	sdk "github.com/pokt-network/pocket-core/types"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	pvm "github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/proxy"
	dbm "github.com/tendermint/tm-db"
)

type AppCreator func(log.Logger, dbm.DB, io.Writer) *PocketCoreApp

func NewClient(c config, creator AppCreator) (*node.Node, *PocketCoreApp, error) {
	// setup the database
	db, err := OpenDB(GlobalConfig)
	if err != nil {
		return nil, nil, err
	}
	// open the tracewriter
	traceWriter, err := openTraceWriter(c.TraceWriter)
	if err != nil {
		return nil, nil, err
	}
	// load the node key
	nodeKey, err := p2p.LoadOrGenNodeKey(c.TmConfig.NodeKeyFile())
	if err != nil {
		return nil, nil, err
	}
	// upgrade the privVal file
	app := creator(c.Logger, db, traceWriter)

	// create & start tendermint node
	c.TmConfig.TxIndex.Indexer = ""
	tmNode, err := node.NewNode(
		c.TmConfig,
		pvm.LoadOrGenFilePV(c.TmConfig.PrivValidatorKeyFile(), c.TmConfig.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewLocalClientCreator(app),
		node.DefaultGenesisDocProviderFunc(c.TmConfig),
		node.DefaultDBProvider,
		node.DefaultMetricsProvider(c.TmConfig.Instrumentation),
		c.Logger.With("module", "node"),
	)
	if err != nil {
		return nil, nil, err
	}
	c.TmConfig.TxIndex.Indexer = "kv"
	// app.SetTxIndexer(tmNode.TxIndexer())
	store, err := node.DefaultDBProvider(&node.DBContext{"tx_index", c.TmConfig})
	if err != nil {
		return nil, nil, err
	}
	fmt.Println(c.TmConfig.TxIndex.IndexKeys)
	app.SetTxIndexer(sdk.NewTxIndex(store, app.cdc, 1, sdk.IndexEvents(splitAndTrimEmpty(c.TmConfig.TxIndex.IndexKeys, ",", " ")))) // TODO config cache size
	app.SetBlockstore(tmNode.BlockStore())
	app.SetEvidencePool(tmNode.EvidencePool())
	return tmNode, app, nil
}

func OpenDB(config sdk.Config) (dbm.DB, error) {
	dataDir := filepath.Join(config.TendermintConfig.RootDir, GlobalConfig.TendermintConfig.DBPath)
	return sdk.NewLevelDB(sdk.ApplicationDBName, dataDir, config.TendermintConfig.LevelDBOptions.ToGoLevelDBOpts())
}

func openTraceWriter(traceWriterFile string) (w io.Writer, err error) {
	if traceWriterFile != "" {
		w, err = os.OpenFile(
			traceWriterFile,
			os.O_WRONLY|os.O_APPEND|os.O_CREATE,
			0666,
		)
		return
	}
	return
}

func splitAndTrimEmpty(s, sep, cutset string) []string {
	if s == "" {
		return []string{}
	}

	spl := strings.Split(s, sep)
	nonEmptyStrings := make([]string, 0, len(spl))
	for i := 0; i < len(spl); i++ {
		element := strings.Trim(spl[i], cutset)
		if element != "" {
			nonEmptyStrings = append(nonEmptyStrings, element)
		}
	}
	return nonEmptyStrings
}

//// upgradePrivVal converts old priv_validator.json file (prior to Tendermint 0.28)
//// to the new priv_validator_key.json and priv_validator_state.json files.
//func upgradePrivVal(config *cfg.Config) {
//	if _, err := os.Stat(config.OldPrivValidatorFile()); !os.IsNotExist(err) {
//		if oldFilePV, err := pvm.LoadOldFilePV(config.OldPrivValidatorFile()); err == nil {
//			oldFilePV.Upgrade(config.PrivValidatorKeyFile(), config.PrivValidatorStateFile())
//		}
//	}
//}

type config struct {
	TmConfig    *cfg.Config
	Logger      log.Logger
	TraceWriter string
}

func UnsafeRollbackData(config *cfg.Config, modifyStateFile bool, height int64) error {
	blockStore, state, blockStoreDB, stateDb, err := state2.BlocksAndStateFromDB(config, state2.DefaultDBProvider)
	if err != nil {
		return err
	}
	// Make Evidence Reactor

	_, evidencePool, err := node.CreateEvidenceReactor(config, node.DefaultDBProvider, stateDb, log.NewNopLogger())
	if err != nil {
		return err
	}
	lastHeight := state.LastBlockHeight
	// if rollback height is less than state.height
	if lastHeight > height {
		// get the state at that height
		stateRestore := state2.RestoreStateFromBlock(stateDb, blockStore, height)
		// save the state to the db to overwrite the current state
		state2.SaveState(stateDb, stateRestore)
		// roll back the blockstore to the previous height
		store.BlockStoreStateJSON{Height: height}.Save(blockStoreDB)
		if modifyStateFile {
			err := modifyPrivValidatorsFile(config, height)
			if err != nil {
				return err
			}
		}
		evidencePool.RollbackEvidence(height, lastHeight)
	} else {
		return fmt.Errorf("the rollback height: %d must be greater than that of the previous state: %d", state.LastBlockHeight, height)
	}
	return nil
}

func modifyPrivValidatorsFile(config *cfg.Config, rollbackHeight int64) error {
	var sig []byte
	filePv := pvm.LoadOrGenFilePV(config.PrivValidatorKeyFile(), config.PrivValidatorStateFile())
	filePv.LastSignState.Height = rollbackHeight
	filePv.LastSignState.Round = 0
	filePv.LastSignState.Step = 0
	filePv.LastSignState.Signature = sig
	filePv.LastSignState.SignBytes = nil
	filePv.Save()
	return nil
}
