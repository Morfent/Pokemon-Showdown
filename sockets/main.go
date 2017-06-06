package main

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"path/filepath"

	"github.com/Zarel/Pokemon-Showdown/sockets/lib"

	"github.com/gorilla/mux"
	"github.com/igm/sockjs-go/sockjs"
)

func main() {
	// Parse our config settings passed through the $PS_CONFIG environment
	// variable by the parent process.
	config, err := sockets.NewConfig("PS_CONFIG")
	if err != nil {
		log.Fatalf("Sockets: failed to read parent's config settings from environment: %v")
	}

	// Instantiate the socket multiplexer and IPC struct.
	smux := sockets.NewMultiplexer()
	conn, err := sockets.NewConnection("PS_IPC_PORT")
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer conn.Close()

	// Begin listening for incoming messages from sockets and the TCP
	// connection to the parent process. For now, they'll just get enqueued
	// for workers to manage later.
	smux.Listen(conn)
	conn.Listen(smux)

	// Set up routing.
	r := mux.NewRouter()

	opts := sockjs.Options{
		SockJSURL:       "//play.pokemonshowdown.com/js/lib/sockjs-1.1.1-nwjsfix.min.js",
		Websocket:       true,
		HeartbeatDelay:  sockjs.DefaultOptions.HeartbeatDelay,
		DisconnectDelay: sockjs.DefaultOptions.DisconnectDelay,
		JSessionID:      sockjs.DefaultOptions.JSessionID,
	}

	r.PathPrefix("/showdown").
		Handler(sockjs.NewHandler("/showdown", opts, smux.Handler))

	customCssDir, _ := filepath.Abs("./config")
	r.Handle("/custom.css", http.FileServer(http.Dir(customCssDir)))

	avatarDir, _ := filepath.Abs("./config/avatars")
	r.PathPrefix("/avatars/").
		Handler(http.StripPrefix("/avatars/", http.FileServer(http.Dir(avatarDir))))

	indexPath, _ := filepath.Abs("./static/index.html")
	r.PathPrefix("/{roomid:[A-Za-z0-9][A-Za-z0-9-]*}").
		HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, indexPath)
		})

	notFoundPath, _ := filepath.Abs("./static/404.html")
	notFoundPage, _ := ioutil.ReadFile(notFoundPath)
	r.NotFoundHandler =
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write(notFoundPage)
		})

	staticDir, _ := filepath.Abs("./static")
	r.Handle("/", http.FileServer(http.Dir(staticDir)))

	// Begin serving over HTTP.
	go func(ba string, port string) {
		srv := &http.Server{
			Handler: r,
			Addr:    ba + port,
		}

		addr, err := net.ResolveTCPAddr("tcp4", ba+port)
		if err != nil {
			log.Fatalf("Sockets: failed to resolve the TCP address of the parent's server: %v", err)
		}

		ln, err := net.ListenTCP("tcp4", addr)
		defer ln.Close()
		if err != nil {
			log.Fatalf("Sockets: failed to listen on %v over HTTP", srv.Addr)
		}

		fmt.Printf("Go workers now listening on %v%v\n", ba, port)

		if ba == "0.0.0.0" {
			fmt.Printf("Test your server at http://localhost%v/\n", port)
		} else {
			fmt.Printf("Test your server at http://%v%v/\n", ba, port)
		}

		if err = http.Serve(ln, r); err != nil {
			log.Fatalf("Sockets: HTTP server failed with error: %v", err)
		}
	}(config.BindAddress, config.Port)

	// Begin serving over HTTPS if configured to do so.
	if config.SSL.Options.Cert != "" && config.SSL.Options.Key != "" {
		go func(ba string, port string, cert string, key string) {
			certs, err := tls.LoadX509KeyPair(cert, key)
			if err != nil {
				log.Fatalf("Sockets: failed to load certificate and key files for TLS: %v", err)
			}

			srv := &http.Server{
				Handler:   r,
				Addr:      ba + port,
				TLSConfig: &tls.Config{Certificates: []tls.Certificate{certs}},
			}

			var ln net.Listener
			ln, err = tls.Listen("tcp4", srv.Addr, srv.TLSConfig)
			defer ln.Close()
			if err != nil {
				log.Fatalf("Sockets: failed to listen on %v over HTTPS", srv.Addr)
			}

			fmt.Printf("Go workers now listening for SSL on port %v\n", port)

			if err := http.Serve(ln, r); err != nil {
				log.Fatalf("Sockets: HTTPS server failed: %v", err)
			}
		}(config.BindAddress, config.SSL.Port, config.SSL.Options.Cert, config.SSL.Options.Key)
	}

	// Finally, spawn workers.to pipe messages received at the multiplexer or
	// IPC connection to each other concurrently.
	master := sockets.NewMaster(config.Workers)
	master.Spawn()
	master.Listen()
}
