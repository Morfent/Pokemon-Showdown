package sockets

import (
	"encoding/json"
	"os"
)

type config struct {
	Workers     int     `json:"workers"`
	Port        string  `json:"port"`
	BindAddress string  `json:"bindAddress"`
	SSL         sslOpts `json:"ssl"`
}

type sslOpts struct {
	Port    string  `json:"port"`
	Options sslKeys `json:"options"`
}

type sslKeys struct {
	Cert string `json:"cert"`
	Key  string `json:"key"`
}

func NewConfig(envVar string) (c config, err error) {
	configEnv := os.Getenv(envVar)
	err = json.Unmarshal([]byte(configEnv), &c)
	return
}
