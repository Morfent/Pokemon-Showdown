/**
 * Config
 * https://pokemonshowdown.com/
 *
 * Config is a struct representing the config settings the parent process
 * passed to us by stringifying pertinent settings as JSON and assigning it to
 * the $PS_CONFIG environment variable.
 */

package sockets

import (
	"encoding/json"
	"os"
)

type config struct {
	// The number of workers for the master to spawn.
	Workers int `json:"workers"`
	// The port the HTTP server will host over.
	Port string `json:"port"`
	// The hostname the HTTP and HTTPS servers will host with.
	BindAddress string `json:"bindAddress"`
	// HTTPS config settings.
	SSL sslOpts `json:"ssl"`
}

type sslOpts struct {
	// The port the HTTPS server will host over.
	Port string `json:"port"`
	// SSL certificate settings.
	Options sslKeys `json:"options"`
}

type sslKeys struct {
	// The path to the SSL certificate file.
	Cert string `json:"cert"`
	// The path to the SSL key file.
	Key string `json:"key"`
}

func NewConfig(envVar string) (c config, err error) {
	configEnv := os.Getenv(envVar)
	err = json.Unmarshal([]byte(configEnv), &c)
	return
}
