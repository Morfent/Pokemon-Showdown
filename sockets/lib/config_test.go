package sockets

import (
	"encoding/json"
	"fmt"
	"testing"
)

func newTestConfig(w int, p string, ba string, s interface{}) (c config) {
	c = config{
		Workers:     w,
		Port:        p,
		BindAddress: ba}
	if ssl, ok := s.(sslOpts); ok {
		c.SSL = ssl
	}
	return
}

func TestConfig(t *testing.T) {
	t.Parallel()
	ws := []int{1, 2, 3, 4}
	ps := []string{":1000", ":2000", ":4000", ":8000"}
	bas := []string{"127.0.0.1", "0.0.0.0", "192.168.0.1", "localhost"}
	ssl := sslOpts{Port: ":443", Options: sslKeys{Cert: "", Key: ""}}
	for _, w := range ws {
		for _, p := range ps {
			for _, ba := range bas {
				t.Run(fmt.Sprintf("%v %v%v", w, ba, p, ssl), func(t *testing.T) {
					go func(w int, p string, ba string, ssl sslOpts) {
						c := newTestConfig(w, p, ba, ssl)
						if _, err := json.Marshal(c); err != nil {
							t.Errorf("Config: failed to stringify config JSON: %v", err)
						}
					}(w, p, ba, ssl)
				})
			}
		}
	}
}
