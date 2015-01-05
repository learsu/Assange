package blockdata

import (
	"Assange/config"
	. "Assange/logging"
	. "Assange/util"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	//"github.com/conformal/btcutil"
	"errors"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
	"github.com/coopernurse/gorp"
	_ "github.com/go-sql-driver/mysql"
	. "strconv"
	"time"
)

type ModelBlock struct {
	Id int64

	//Block info
	Height     int64
	Hash       string
	PrevHash   string
	NextHash   string
	MerkleRoot string
	Time       time.Time
	Ver        uint32
	Nonce      uint32
	Bits       uint32

	//Transactions
	Txs []*ModelTx `db:"-"`

	//More flags to be added
	ConfirmFlag bool
}

type ModelTx struct {
	Id int64

	//Transaction info
	IsCoinbase bool
	Hash       string

	//Spenditems
	SItems []*ModelSpendItem `db:"-"`

	//More flags to be added
}

type RelationBlockTx struct {
	Id int64

	//Including relation between block and tx
	BlockId int64
	TxId    int64

	//More flags to be added
}

type ModelAddressBalance struct {
	Id int64

	Address string
	Balance int64
}

type ModelSpendItem struct {
	Id int64

	//Transaction output info
	OutTxId    int64
	IsCoinbase bool
	OutScript  []byte
	Address    string
	Value      int64
	OutIndex   int64

	//Transaction input info
	InTxId       int64
	InScript     []byte
	PrevOutHash  string `db:"-"`
	PrevOutIndex int64  `db:"-"`

	//More flags to be added
	IsOut bool `db:"-"`
}

var log = GetLogger("DB", DEBUG)

func InitDb(config config.Configuration) (*gorp.DbMap, error) {
	source := fmt.Sprintf("%s:%s@tcp(%s:3306)/%s?parseTime=True", config.Db_user, config.Db_password, config.Db_host, config.Db_database)
	db, err := sql.Open("mysql", source)
	if err != nil {
		return nil, err
	}
	log.Info("Connect to database server:%s", config.Db_host)
	return &gorp.DbMap{Db: db, Dialect: gorp.MySQLDialect{"InnoDB", "UTF8"}}, nil
}

func InitTables(dbmap *gorp.DbMap) error {
	dbmap.AddTableWithName(ModelBlock{}, "block").SetKeys(true, "Id")
	dbmap.AddTableWithName(ModelTx{}, "tx").SetKeys(true, "Id")
	dbmap.AddTableWithName(RelationBlockTx{}, "block_tx").SetKeys(true, "Id")
	dbmap.AddTableWithName(ModelSpendItem{}, "spenditem").SetKeys(true, "Id")
	dbmap.AddTableWithName(ModelAddressBalance{}, "balance").SetKeys(true, "Id")
	err := dbmap.CreateTablesIfNotExists()
	if err != nil {
		return err
	}

	dbmap.Exec("create unique index uidx_block_hash on block(Hash)")
	dbmap.Exec("alter table `block` add index `idx_block_prevhash` (PrevHash)")
	dbmap.Exec("alter table `block` add index `idx_block_nexthash` (NextHash)")
	dbmap.Exec("alter table `block` add index `idx_block_height` (Height)")

	dbmap.Exec("create unique index uidx_tx_hash on tx(Hash)")

	dbmap.Exec("create unique index uidx_spenditem_outtxid_outindex on spenditem(OutTxId,OutIndex)")
	dbmap.Exec("alter table `spenditem` add index `idx_spenditem_address` (Address)")

	dbmap.Exec("create unique index uidx_balance_address on balance(Address)")

	return nil
}

func GetMaxBlockHeightFromDB(dbmap *gorp.DbMap) (int64, error) {
	//var blkBuff []*ModleBlock
	var maxHeight int64
	maxHeight, _ = dbmap.SelectInt("select max(Height) as Height from block")
	if maxHeight == 0 {
		rowCount, _ := dbmap.SelectInt("select count(*) from block")
		if rowCount == 0 {
			maxHeight = -1
			log.Info("No record in block database. Set max height to -1.")
		}
	}
	log.Info("Max height in block database is %d.", maxHeight)
	return maxHeight, nil
}

func GetMaxTxIdFromDB(dbmap *gorp.DbMap) (int64, error) {
	var maxTxId int64
	maxTxId, _ = dbmap.SelectInt("select max(Id) as Id from tx")
	log.Info("Max id in transaction database is %d.", maxTxId)
	return maxTxId, nil
}

func InsertTxIntoDB(trans *gorp.Transaction, tx *ModelTx) *ModelTx {
	var txBuff []*ModelTx
	oldLen := len(txBuff)
	trans.Select(&txBuff, "select * from tx where Hash=?", tx.Hash)
	if oldLen == len(txBuff) {
		err := trans.Insert(tx)
		if err != nil {
			log.Error(err.Error())
		}
		return tx
	} else {
		log.Error("Tx hash already existed. Id:%d, Hash:%s.", tx.Id, tx.Hash)
		return txBuff[oldLen]
	}
}

func InsertBlockIntoDB(trans *gorp.Transaction, block *ModelBlock) {
	err := trans.Insert(block)
	if err != nil {
		log.Error(err.Error())
		return
	}
	log.Info("Insert new block, Id:%d, Height:%d.", block.Id, block.Height)
	for idx, _ := range block.Txs {
		block.Txs[idx] = InsertTxIntoDB(trans, block.Txs[idx])
		blockTx := new(RelationBlockTx)
		blockTx.BlockId = block.Id
		blockTx.TxId = block.Txs[idx].Id
		err := trans.Insert(blockTx)
		if err != nil {
			log.Error(err.Error())
		}
	}
}

func NewTxFromString(result string, tx *ModelTx) {
	bytesResult, _ := hex.DecodeString(result)
	tx1, err := btcutil.NewTxFromBytes(bytesResult)
	if err != nil {
		log.Error(err.Error())
	}
	msgtx := tx1.MsgTx()
	tx.SItems = NewSpendItemsFromMsg(msgtx, tx)
}

func NewBlockFromMap(resultMap map[string]interface{}) (*ModelBlock, error) {
	block := new(ModelBlock)
	block.Height, _ = ParseInt(string(resultMap["height"].(json.Number)), 10, 64)
	block.Hash = resultMap["hash"].(string)
	if _, ok := resultMap["previousblockhash"]; ok {
		block.PrevHash = resultMap["previousblockhash"].(string)
	}
	if _, ok := resultMap["nextblockhash"]; ok {
		block.NextHash = resultMap["nextblockhash"].(string)
	}
	block.MerkleRoot = resultMap["merkleroot"].(string)
	timeUint64, _ := ParseInt(string(resultMap["time"].(json.Number)), 10, 64)
	block.Time = time.Unix(timeUint64, 0)
	verUint64, _ := ParseUint(string(resultMap["version"].(json.Number)), 10, 32)
	block.Ver = uint32(verUint64)
	nonceUint64, _ := ParseUint(string(resultMap["nonce"].(json.Number)), 10, 32)
	block.Nonce = uint32(nonceUint64)
	bitsUint64, _ := ParseUint(resultMap["bits"].(string), 16, 32)
	block.Bits = uint32(bitsUint64)
	block.ConfirmFlag = true

	txsResult, _ := resultMap["tx"].([]interface{})
	for idx, txResult := range txsResult {
		tx := new(ModelTx)
		tx.Hash = txResult.(string)
		if idx == 0 {
			tx.IsCoinbase = true
		} else {
			tx.IsCoinbase = false
		}
		block.Txs = append(block.Txs, tx)
	}
	return block, nil
}

func (s *ModelSpendItem) NewModelSpendItem(result string) ([]*btcwire.MsgTx, error) {
	return nil, nil
}

func GetAddressBalance(trans *gorp.Transaction, address string) (*ModelAddressBalance, error) {
	var balanceBuff []*ModelAddressBalance
	oldLen := len(balanceBuff)
	_, err := trans.Select(&balanceBuff, "select * from balance where Address=?", address)
	if err != nil {
		return nil, err
	}
	balanceBuff = balanceBuff[oldLen:len(balanceBuff)]

	if len(balanceBuff) == 1 {
		log.Debug("Address:%s found in database.", address)
		return balanceBuff[0], nil
	} else if len(balanceBuff) > 1 {
		return nil, errors.New("Multiple addresses found")
	} else {
		b := new(ModelAddressBalance)
		b.Address = address
		b.Balance = 0
		trans.Insert(b)
		log.Debug("New balance record. Address:%s.", b.Address)
		return b, nil
	}
}

func NewSpendItemsFromMsg(msgTx *btcwire.MsgTx, mtx *ModelTx) []*ModelSpendItem {
	var sBuff []*ModelSpendItem
	//Handle transaction output recursivly.
	for idx, out := range msgTx.TxOut {
		s := new(ModelSpendItem)
		s.IsOut = true
		s.OutTxId = mtx.Id
		s.IsCoinbase = mtx.IsCoinbase
		s.OutScript = out.PkScript
		s.Value = out.Value
		s.OutIndex = int64(idx)
		//Extract address
		address, err := ExtractAddrFromScript(s.OutScript)
		if err != nil {
			log.Error(err.Error())
		}
		s.Address = address
		sBuff = append(sBuff, s)
	}

	//Handle transaction input recursibly.
	for _, in := range msgTx.TxIn {
		s := new(ModelSpendItem)
		s.IsCoinbase = mtx.IsCoinbase
		s.IsOut = false
		s.PrevOutHash = in.PreviousOutPoint.Hash.String()
		s.PrevOutIndex = int64(in.PreviousOutPoint.Index)
		s.InTxId = mtx.Id
		s.InScript = in.SignatureScript
		sBuff = append(sBuff, s)
	}
	return sBuff
}

func InsertSpendItemsIntoDB(trans *gorp.Transaction, sItem []*ModelSpendItem) error {
	var sBuff []*ModelSpendItem
	for _, s := range sItem {
		if s.IsOut {
			err := trans.Insert(s)
			if err != nil {
				log.Error(err.Error())
				log.Error("Insert spenditem error. Output tx id:%d, value:%d, index:%d, address:%s.", s.OutTxId, s.Value, s.OutIndex, s.Address)
				continue
			}
			log.Debug("New spenditem into database. Output tx id:%d, value:%d, index:%d, address:%s.", s.OutTxId, s.Value, s.OutIndex, s.Address)
			UpdateBalance(trans, s.Address, s.Value, true)
		} else {
			if s.IsCoinbase {
				return nil
			}
			prevTxId, _ := trans.SelectInt("select Id from tx where Hash=?", s.PrevOutHash)
			oldLen := len(sBuff)
			query := fmt.Sprintf("select * from spenditem where OutTxId=%d and OutIndex=%d", prevTxId, s.PrevOutIndex)
			_, err := trans.Select(&sBuff, query)
			if err != nil {
				log.Error(err.Error())
			}
			sBuff = sBuff[oldLen:len(sBuff)]

			//update the spenditem
			if len(sBuff) == 1 {
				sBuff[0].InTxId = s.InTxId
				sBuff[0].InScript = s.InScript
				trans.Update(sBuff[0])
				log.Debug("Update spenditem Id:%d, OutTxId:%d. Set InTxId=%d.", sBuff[0].Id, sBuff[0].OutTxId, s.InTxId)
				UpdateBalance(trans, sBuff[0].Address, sBuff[0].Value, false)
			} else if len(sBuff) > 1 {
				log.Error("Multiple outputs matched for input previous tx, OutTxId=%d, index=%d.", prevTxId, s.PrevOutIndex)
			} else {
				log.Error("No output found in database. OutTxId=%d, index=%d.", prevTxId, s.PrevOutIndex)
			}
		}
	}
	return nil
}

func UpdateBalance(trans *gorp.Transaction, address string, value int64, flag bool) error {
	b, err := GetAddressBalance(trans, address)
	if err != nil {
		return err
	}
	oldBalance := b.Balance
	if flag {
		b.Balance += value
	} else {
		b.Balance -= value
	}
	trans.Update(b)
	log.Debug("Update address:%s balance from %d to %d.", b.Address, oldBalance, b.Balance)
	return nil
}
