package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/gorilla/websocket"
)

// see gorilla websocket chat example on github
const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
)

var tmpl *template.Template

type hub struct {
	// 3 maps; one for the phones, one for remote control (=admin)
	// one for mapping between phone id and its connection
	phoneMapping      map[string]*connection
	phoneConnections  map[*connection]bool
	adminConnections  map[*connection]bool
	registerPhone     chan *connection
	registerAdmin     chan *connection
	unregisterPhone   chan *connection
	unregisterAdmin   chan *connection
	sendToPhone       chan *toggleRecord
	broadcastToAdmin  chan *serverMessage
	broadcastToPhones chan *toggleRecord
}

var h = hub{
	// phones have additional map linking their clientid to a connection
	phoneMapping:      make(map[string]*connection),
	phoneConnections:  make(map[*connection]bool),
	adminConnections:  make(map[*connection]bool),
	registerPhone:     make(chan *connection),
	registerAdmin:     make(chan *connection),
	unregisterPhone:   make(chan *connection),
	unregisterAdmin:   make(chan *connection),
	sendToPhone:       make(chan *toggleRecord),
	broadcastToAdmin:  make(chan *serverMessage),
	broadcastToPhones: make(chan *toggleRecord),
}

func (h *hub) phoneSend(message *toggleRecord) {
	if con, ok := h.phoneMapping[message.ClientId]; ok {
		con.send <- message
	}
}

func (h *hub) phoneBroadcast(message *toggleRecord) {
	for key, _ := range h.phoneConnections {
		key.send <- message
	}
}

func (h *hub) adminBroadcast(message *serverMessage) {
	for key, _ := range h.adminConnections {
		key.send <- message
	}
}

func (h *hub) run() {
	for {
		select {
		case c := <-h.registerPhone:
			h.phoneConnections[c] = true
			// keep track of phonemapping upon registration message
			// notification of admins also happens over there -> see readPump

		case c := <-h.unregisterPhone:
			if _, ok := h.phoneConnections[c]; ok {
				delete(h.phoneConnections, c)
				close(c.send)
				if c.id != "" {
					if _, containsId := h.phoneMapping[c.id]; containsId {
						delete(h.phoneMapping, c.id)
					}
					// broadcast to admins
					// commented line below won't work since reads and writes on channel are blocking and select only executes 1
					// h.broadcastToAdmin <- serverMessage{c.id, "ff", 0}
					h.adminBroadcast(&serverMessage{c.id, c.deviceType, 0})
				}
			}

		case c := <-h.registerAdmin:
			h.adminConnections[c] = true

		case c := <-h.unregisterAdmin:
			if _, ok := h.adminConnections[c]; ok {
				delete(h.adminConnections, c)
				close(c.send)
			}

		case message := <-h.sendToPhone:
			h.phoneSend(message)

		case message := <-h.broadcastToAdmin:
			h.adminBroadcast(message)

		case message := <-h.broadcastToPhones:
			h.phoneBroadcast(message)
		}
	}
}

// message types for json websocket communication

// these implement the empty interface: interface{} -> type of connection.send channel
// capitals since go only exports fields with capitals; otherwise json marshaling is not working
// no spaces between json: and subsequent string, won't work then
type registration struct {
	CameraResolution string `json:"cameraResolution"`
	ClientId         string `json:"client_id"`
	Cpu              string `json:"cpu"`
	Message          string `json:"message"`
	UserAgent        string `json:"user-agent"`
}

type toggleRecord struct {
	ToggleRecord int    `json:"toggleRecord"`
	ClientId     string `json:"client_id"`
}

type serverMessage struct {
	ClientId    string `json:"client_id"`
	Device      string `json:"device"`
	IsConnected int    `json:"isConnected"`
}

// end of message types

type connection struct {
	socket     *websocket.Conn
	send       chan interface{}
	id         string
	isPhone    bool
	deviceType string
}

func (c *connection) readPump() {
	defer func() {
		if c.isPhone {
			h.unregisterPhone <- c
		} else {
			h.unregisterAdmin <- c
		}
		c.socket.Close()
	}()
	c.socket.SetReadDeadline(time.Now().Add(pongWait))
	c.socket.SetPongHandler(func(string) error {
		c.socket.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		if c.isPhone {
			var message registration
			if err := c.socket.ReadJSON(&message); err != nil {
				break
			}
			// add clientId to connection and add to phoneMapping
			c.id = message.ClientId
			h.phoneMapping[c.id] = c
			// broadcast this to admins
			textArray := strings.Split(message.UserAgent, ";")
			c.deviceType = textArray[len(textArray)-1]
			var serverMesg = serverMessage{message.ClientId, c.deviceType, 1}
			h.broadcastToAdmin <- &serverMesg
		} else {
			var message toggleRecord
			if err := c.socket.ReadJSON(&message); err != nil {
				break
			}
			// send to right client; if client_id = "all" broadcast to all phones
			if message.ClientId == "all" {
				h.broadcastToPhones <- &message
			} else {
				h.sendToPhone <- &message
			}
		}
	}
}

func (c *connection) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.socket.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				return
			}
			if err := c.socket.WriteJSON(message); err != nil {
				log.Println(err)
				return

			}
		case <-ticker.C:
			if err := c.socket.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		// match static files
		http.ServeFile(w, r, r.URL.Path[1:])
		// 404's are returned by serveFile
		// http.Error(w, "Not found", 404)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	log.Println("Upgraded connection")
	c := &connection{socket: conn, isPhone: true, send: make(chan interface{})}
	h.registerPhone <- c
	go c.writePump()
	c.readPump()
}

func serveAdmin(w http.ResponseWriter, r *http.Request) {
	// both http and websocket upgrade are gets
	if r.Method != "GET" {
		log.Println(r)
		http.Error(w, "Method not allowed", 405)
		return
	}
	// upgrade for websockets
	if r.Header.Get("Upgrade") != "" {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil { //not the upgrade request but regular http??
			log.Println(err)
			return
		}
		log.Println("Upgraded connection")
		c := &connection{socket: conn, isPhone: false, send: make(chan interface{})}
		h.registerAdmin <- c
		go c.writePump()
		c.readPump()
		// else: no upgrade header: simply serve page
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// get existing phone connections in proper form
		var connectedPhones = make(map[string]*serverMessage)
		for key, _ := range h.phoneConnections {
			connectedPhones[key.id] = &serverMessage{key.id, key.deviceType, 1}
		}
		// convert connectedPhones to json string to inject it in javascript
		var connectedPhonesString string
		if val, err := json.Marshal(connectedPhones); err == nil {
			connectedPhonesString = string(val)
		}
		tmpl.Execute(w, struct {
			Title   string
			Clients string
		}{"Selvie Remote", connectedPhonesString})
	}
}

func listenForInput() {
	scanner := bufio.NewScanner(os.Stdin)
	i := 1
	for {
		fmt.Print("enter text: ")
		scanner.Scan()
		// entered := scanner.Text()
		h.broadcastToPhones <- &toggleRecord{ToggleRecord: i, ClientId: "all"}
		i = 1 - i // toggle between zero and one
	}
}

func main() {
	// command line flags
	port := flag.Int("port", 3001, "port to serve on")
	flag.Parse()
	// name of this new template matters, probably because we load only one file??
	// change delimiters so we don't interfere with js templates
	temp := template.New("admin.html")
	temp.Delims("[[", "]]")
	tmpl, _ = temp.ParseFiles("admin.html")
	go h.run()
	http.HandleFunc("/", wsHandler)
	http.HandleFunc("/admin", serveAdmin)
	log.Printf("Running on port %d\n", *port)
	go listenForInput()
	address := fmt.Sprintf(":%d", *port)
	err := http.ListenAndServe(address, nil)
	fmt.Println(err.Error())
}
