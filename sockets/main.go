package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"

	"github.com/Zarel/Pokemon-Showdown/sockets/lib"

	routing "github.com/gorilla/mux"
	"github.com/igm/sockjs-go/sockjs"
)

func notFoundHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/404.html", http.StatusSeeOther)
}

func main() {
	// Parse our config settings passed through the $PS_CONFIG environment
	// variable by the parent process.
	config, err := sockets.NewConfig("PS_CONFIG")
	if err != nil {
		log.Fatal("Sockets: failed to read parent's config settings from environment")
	}

	// Instantiate the socket multiplexer and IPC struct..
	mux := sockets.NewMultiplexer()
	conn, err := sockets.NewConnection("PS_IPC_PORT")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// Begin listening for incoming messages from sockets and the TCP
	// connection to the parent process. For now, they'll just get enqueued
	// for workers to manage later..
	mux.Listen(conn)
	err = conn.Listen(mux)
	if err != nil {
		log.Fatal("%v", err)
	}

	// Set up server routing.
	r := routing.NewRouter()

	avatarDir, _ := filepath.Abs("./config/avatars")
	r.PathPrefix("/avatars/").
		Handler(http.FileServer(http.Dir(avatarDir)))

	customCSSDir, _ := filepath.Abs("./config")
	r.Handle("/custom.css", http.FileServer(http.Dir(customCSSDir)))

	// Set up the SockJS server.
	opts := sockjs.Options{
		SockJSURL:       "//play.pokemonshowdown.com/js/lib/sockjs-1.1.1-nwjsfix.min.js",
		Websocket:       true,
		HeartbeatDelay:  sockjs.DefaultOptions.HeartbeatDelay,
		DisconnectDelay: sockjs.DefaultOptions.DisconnectDelay,
		JSessionID:      sockjs.DefaultOptions.JSessionID}

	r.PathPrefix("/showdown").
		Handler(sockjs.NewHandler("/showdown", opts, mux.Handler))

	staticDir, _ := filepath.Abs("/static")
	r.Handle("/", http.StripPrefix("/static", http.FileServer(http.Dir(staticDir))))

	r.NotFoundHandler = http.HandlerFunc(notFoundHandler)

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
				TLSConfig: &tls.Config{Certificates: []tls.Certificate{certs}}}

			var ln net.Listener
			ln, err = tls.Listen("tcp4", srv.Addr, srv.TLSConfig)
			defer ln.Close()
			if err != nil {
				log.Fatalf("Sockets: failed to listen on %v over HTTPS", srv.Addr)
			}

			fmt.Printf("Sockets: now serving on https://%v%v/\n", ba, port)
			log.Fatal(http.Serve(ln, r))
		}(config.BindAddress, config.SSL.Port, config.SSL.Options.Cert, config.SSL.Options.Key)
	}

	// Begin serving over HTTP.
	go func(ba string, port string) {
		srv := &http.Server{
			Handler: r,
			Addr:    ba + port}

		ln, err := net.Listen("tcp4", srv.Addr)
		defer ln.Close()
		if err != nil {
			log.Fatalf("Sockets: failed to listen on %v over HTTP", srv.Addr)
		}

		fmt.Printf("Sockets: now serving on http://%v%v/\n", ba, port)
		log.Fatal(http.Serve(ln, r))
	}(config.BindAddress, config.Port)

	// Finally, spawn workers.to pipe messages received at the multiplexer or
	// IPC connection to each other concurrently.
	master := sockets.NewMaster(config.Workers)
	master.Spawn()
	master.Listen()
}
