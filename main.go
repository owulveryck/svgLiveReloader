package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"nhooyr.io/websocket"
)

//go:embed assets/*
var assets embed.FS

var fullPath string

func main() {
	port, ok := os.LookupEnv("PORT")
	if !ok {
		port = "8080"
	}

	if len(os.Args) != 2 {
		log.Fatalf("usage: %v [svg file to watch]", os.Args[0])
	}
	fileToWatch := os.Args[1]
	// Get the directory
	var err error
	fullPath, err = filepath.Abs(fileToWatch)
	if err != nil {
		log.Fatal(err)
	}
	pathToWatch, err := filepath.Abs(filepath.Dir(fileToWatch))
	if err != nil {
		log.Fatal(err)
	}
	commC := make(chan string)

	// Create new watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	// Start listening for events.
	go func() {
		// send initial image
		commC <- fullPath
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					if event.Name == fullPath {
						log.Println(event.Name)
						commC <- event.Name
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
			}
		}
	}()

	// Add a path.
	log.Println("Watching " + pathToWatch)
	err = watcher.Add(pathToWatch)
	if err != nil {
		log.Fatal(err)
	}

	ws := &wsWriter{
		C: commC,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", ws.handler)

	myFs, err := fs.Sub(assets, "assets")
	if err != nil {
		log.Fatal(err)
	}
	assetsFs := http.FileServer(http.FS(myFs))

	mux.Handle("/", http.StripPrefix("/", assetsFs))
	log.Println("listening on " + port + ". Use the PORT env var to change it")
	err = http.ListenAndServe(":"+port, mux)
	log.Fatal(err)
}

type wsWriter struct {
	C <-chan string
}

// This handler demonstrates how to correctly handle a write only WebSocket connection.
// i.e you only expect to write messages and do not expect to read any messages.
func (ws *wsWriter) handler(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer c.Close(websocket.StatusInternalError, "the sky is falling")

	ctx, cancel := context.WithTimeout(r.Context(), time.Minute*10)
	defer cancel()

	ctx = c.CloseRead(ctx)

	for {
		select {
		case <-ctx.Done():
			c.Close(websocket.StatusNormalClosure, "")
			return
		case file := <-ws.C:
			svg, err := ioutil.ReadFile(file)
			if err != nil {
				log.Println(err)
				return
			}
			w, err := c.Writer(ctx, websocket.MessageText)
			if err != nil {
				log.Println(err)
				return
			}
			fmt.Fprintf(w, "%s", svg)
			w.Close()
		}
	}
}
