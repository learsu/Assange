package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
)

type Configuration struct {
	//Database config
	Db_host     string
	Db_user     string
	Db_password string
	Db_database string

	//Explorer database config
	Explorer_user     string
	Explorer_password string

	//RPC config
	Rpc_host     string
	Rpc_port     int
	Rpc_user     string
	Rpc_password string

	//Block data file config
	Block_data_dir string
}

func InitConfiguration(fname string) (Configuration, error) {
	content, err := ioutil.ReadFile(fname)
	if err != nil {
		fmt.Print("Error:", err)
	}
	var conf Configuration
	err = json.Unmarshal([]byte(content), &conf)
	if err != nil {
		fmt.Print("Error:", err)
	}
	return conf, nil
}
