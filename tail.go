// Golang HTML5 Server Side Events Example
//
// Run this code like:
//  > go run server.go.bak
//
// Then open up your browser to http://localhost:8000
// Your browser must support HTML5 SSE, of course.

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"github.com/hpcloud/tail"
	"github.com/kardianos/service"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

//go:generate go-bindata-assetfs static/...
var (
	hostname string
	port     int
	filename string
	gbk      bool
)

/* register command line options */
func init() {
	flag.StringVar(&hostname, "h", "0.0.0.0", "The hostname or IP on which the server will listen")
	flag.IntVar(&port, "p", 8080, "The port on which the server will listen")
	flag.StringVar(&filename, "f", "log.log", "log filename")
	flag.BoolVar(&gbk, "gbk", false, "log file encoded use GBK")
	if filename == "log.log" && os.Getenv("LOG_FILE") != "" {
		filename = os.Getenv("LOG_FILE")
	}
}

// A single Broker will be created in this program. It is responsible
// for keeping a list of which clients (browsers) are currently attached
// and broadcasting events (messages) to those clients.
//
type Broker struct {
	// Create a map of clients, the keys of the map are the channels
	// over which we can push messages to attached clients.  (The values
	// are just booleans and are meaningless.)
	//
	clients map[chan string]bool

	// Channel into which new clients can be pushed
	//
	newClients chan chan string

	// Channel into which disconnected clients should be pushed
	//
	defunctClients chan chan string

	// Channel into which messages are pushed to be broadcast out
	// to attached clients.
	//
	messages chan string
}

// This Broker method starts a new goroutine.  It handles
// the addition & removal of clients, as well as the broadcasting
// of messages out to clients that are currently attached.
//
func (b *Broker) Start() {

	// Start a goroutine
	//
	go func() {

		// Loop endlessly
		//
		for {

			// Block until we receive from one of the
			// three following channels.
			select {

			case s := <-b.newClients:

				// There is a new client attached and we
				// want to start sending them messages.
				b.clients[s] = true
				log.Println("Added new client")
			case s := <-b.defunctClients:

				// A client has dettached and we want to
				// stop sending them messages.
				delete(b.clients, s)
				close(s)

				log.Println("Removed client")

			case msg := <-b.messages:

				// There is a new message to send.  For each
				// attached client, push the new message
				// into the client's message channel.
				for s := range b.clients {
					s <- msg
				}
			}
		}
	}()
}

// This Broker method handles and HTTP request at the "/events/" URL.
//
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	// Make sure that the writer supports flushing.
	//
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// Create a new channel, over which the broker can
	// send this client messages.
	messageChan := make(chan string)

	// Add this client to the map of those that should
	// receive updates
	b.newClients <- messageChan

	// Listen to the closing of the http connection via the CloseNotifier
	// notify := w.(http.CloseNotifier).CloseNotify()
	notify := r.Context().Done()
	go func() {
		<-notify
		// Remove this client from the map of attached clients
		// when `EventHandler` exits.
		b.defunctClients <- messageChan
		log.Println("HTTP connection just closed.")
	}()

	// Set the headers related to event streaming.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")

	// Don't close the connection, instead loop endlessly.
	for {

		// Read from our messageChan.
		msg, open := <-messageChan

		if !open {
			// If our messageChan was closed, this means that the client has
			// disconnected.
			break
		}

		// Write to the ResponseWriter, `w`.
		fmt.Fprintf(w, "data: %s\n\n", msg)

		// Flush the response.  This is only possible if
		// the response supports streaming.
		f.Flush()
	}

	// Done.
	log.Println("Finished HTTP request at ", r.URL.Path)
}

func handleHistory(w http.ResponseWriter, r *http.Request) {
	file, err := os.Open(filename)
	if err != nil {
		fmt.Fprintf(w, "An error occurred on opening the %s\n"+
			"Does the file exist?\n"+
			"Have you got acces to it?\n"+
			"%s", filename, err.Error())
	}
	defer file.Close()
	var reader io.Reader
	if gbk {
		reader = transform.NewReader(file, simplifiedchinese.GBK.NewDecoder())
	} else {
		reader = bufio.NewReader(file)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("content-disposition", "attachment; filename=\""+filename+"\"")
	io.Copy(w, reader)
}

var logger service.Logger

type program struct {
	httpServer *http.Server
}

func (p *program) Start(s service.Service) error {
	go p.run()
	return nil
}

func (p *program) run() {
	// 运行逻辑
	// Make a new Broker instance
	b := &Broker{
		make(map[chan string]bool),
		make(chan (chan string)),
		make(chan (chan string)),
		make(chan string),
	}

	// Start processing events
	b.Start()

	// Make b the HTTP handler for "/events/".  It can do
	// this because it has a ServeHTTP method.  That method
	// is called in a separate goroutine for each
	// request to "/events/".
	http.Handle("/events/", b)

	// Generate a constant stream of events that get pushed
	// into the Broker's messages channel and are then broadcast
	// out to any clients that are attached.
	go func() {
		tails, err := tail.TailFile(filename, tail.Config{
			ReOpen: true,
			Follow: true,
			// Location:  &tail.SeekInfo{Offset: 0, Whence: 2},
			MustExist: false,
			Poll:      true,
		})
		if err != nil {
			fmt.Println("tail file err:", err)
			return
		}
		for {
			if msg, ok := <-tails.Lines; ok {
				s := msg.Text
				if gbk {
					if result, _, err := transform.String(simplifiedchinese.GBK.NewDecoder(), s); err == nil {
						s = result
					}
				}
				b.messages <- s
			} else {
				fmt.Printf("tail file close reopen, filename:%s\n", tails.Filename)
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
	}()

	var addr = fmt.Sprintf("%s:%d", hostname, port)
	log.Println("service listening on", addr)

	fs := http.FileServer(assetFS())
	http.Handle("/", fs)

	http.HandleFunc("/history", handleHistory)

	// Start the server and listen forever on port 8000.
	p.httpServer = &http.Server{Addr: addr, Handler: nil}
	p.httpServer.ListenAndServe()
}

func (p *program) Stop(s service.Service) error {
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	return p.httpServer.Shutdown(ctx)
}

func main() {
	svcConfig := &service.Config{
		Name:        "LogMonitoring",
		DisplayName: "Log Monitoring",
		Description: "Real-time log monitoring",
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}
	logger, err = s.Logger(nil)
	if err != nil {
		log.Fatal(err)
	}
	args := os.Args
	if len(args) <= 1 {
		if err = s.Run(); err != nil {
			logger.Error(err)
		}
		return
	}
	switch os.Args[1] {
	case "install":
		if err := s.Install(); err != nil {
			logger.Error(err)
		} else {
			logger.Info("install success!")
		}
	case "start":
		if err := s.Start(); err != nil {
			logger.Error(err)
		} else {
			logger.Info("start success!")
		}
	case "stop":
		if err := s.Stop(); err != nil {
			logger.Error(err)
		} else {
			logger.Info("stop success!")
		}
	case "restart":
		if err := s.Restart(); err != nil {
			logger.Error(err)
		} else {
			logger.Info("restart success!")
		}
	case "uninstall":
		if err := s.Uninstall(); err != nil {
			logger.Error(err)
		} else {
			logger.Info("uninstall success!")
		}
	default:
		flag.Usage = usage
		flag.Parse()
		err = s.Run()
		if err != nil {
			logger.Error(err)
		}
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `
Usage: %s [install|start|stop|restart|uninstall] [-f filename] [-gbk] [-h hostname] [-p port]
 
Options:
`, os.Args[0])
	fmt.Fprintf(os.Stderr, "  install\n\tinstall server\n")
	fmt.Fprintf(os.Stderr, "  start\n\tstart server\n")
	fmt.Fprintf(os.Stderr, "  stop\n\tstop server\n")
	fmt.Fprintf(os.Stderr, "  restart\n\trestart server\n")
	fmt.Fprintf(os.Stderr, "  uninstall\n\tuninstall server\n")
	flag.PrintDefaults()
}
