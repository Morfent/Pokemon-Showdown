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
	Workers     int     `json:"workers"`     // Number of workers for the master to spawn.
	Port        string  `json:"port"`        // HTTP server port.
	BindAddress string  `json:"bindAddress"` // HTTP/HTTPS server(s) hostname.
	SSL         sslOpts `json:"ssl"`         // HTTPS config settings.
}

type sslOpts struct {
	Port    string  `json:"port"`    // HTTPS server port.
	Options sslKeys `json:"options"` // SSL config settings.
}

type sslKeys struct {
	Cert string `json:"cert"` // Path to the SSL certificate.
	Key  string `json:"key"`  // Path to the SSL key.
}

func NewConfig(envVar string) (c config, err error) {
	configEnv := os.Getenv(envVar)
	err = json.Unmarshal([]byte(configEnv), &c)
	return
}
