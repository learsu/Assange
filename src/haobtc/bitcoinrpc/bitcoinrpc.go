package bitcoinrpc 

func SearchRawTxs(server string, id int32, address string, skip int32, count int32){
    BitcoinRPC(server, "searchrawtransactions", id, []interface{}{address , 1, skip, count})
}