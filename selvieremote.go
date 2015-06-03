package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
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

	// PING and PONG messages are only necessary to detect unintended closing of the socket;
	// if the client closes the socket we get a +/- immediate readerror on readjson -> we clean the connection
	// but if e.g. connection drops or is closed by proxy, this will be detected via the ping-pong

	// Time allowed to read the next pong message from the peer.
	pongWait = 180 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = 90 * time.Second
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
	sendToPhone       chan *actionOnPhone
	broadcastToAdmin  chan *serverMessage
	broadcastToPhones chan *actionOnPhone
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
	sendToPhone:       make(chan *actionOnPhone),
	broadcastToAdmin:  make(chan *serverMessage),
	broadcastToPhones: make(chan *actionOnPhone),
}

func (h *hub) phoneSend(message *actionOnPhone) {
	if con, ok := h.phoneMapping[message.ClientId]; ok {
		con.send <- message
	}
}

func (h *hub) phoneBroadcast(message *actionOnPhone) {
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
			// keep track of phonemapping upon clientMessage (registration)
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
					h.adminBroadcast(&serverMessage{ClientId: c.id, Device: c.deviceType, IsConnected: "0"})
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

// capitals since go only exports fields with capitals; otherwise json marshaling is not working
// no spaces between json: and subsequent string, won't work then

// incoming from client
// use the following for both registration and status; cf in java :-p
type clientMessage struct {
	CameraResolution string `json:"cameraResolution,omitempty"`
	ClientId         string `json:"client_id"`
	Cpu              string `json:"cpu,omitempty"`
	Message          string `json:"message,omitempty"`
	UserAgent        string `json:"user-agent,omitempty"`
	Status           string `json:"status,omitempty"`
}

// outgoing to client, incoming from admin
type actionOnPhone struct {
	ToggleRecord string `json:"toggleRecord,omitempty"`
	WipeVideos   string `json:"wipeVideos,omitempty"`
	PostLog      string `json:"postLog,omitempty"`
	ReconnectIn  string `json:"reconnectIn,omitempty"`
	ClientId     string `json:"client_id"`
}

// outgoing to admin
// also for connection as well as recording status
type serverMessage struct {
	ClientId    string `json:"client_id"`
	Device      string `json:"device,omitempty"`
	IsConnected string `json:"isConnected,omitempty"` //string since int 0 or bool false are also omitted
	Status      string `json:"status,omitempty"`
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
			var message clientMessage
			if err := c.socket.ReadJSON(&message); err != nil {
				break
			}
			if message.Message == "device_registration" {
				// add clientId to connection and add to phoneMapping
				c.id = message.ClientId
				h.phoneMapping[c.id] = c
				// broadcast this to admins
				textArray := strings.Split(message.UserAgent, ";")
				c.deviceType = textArray[len(textArray)-1]
				var serverMesg = serverMessage{ClientId: message.ClientId, Device: c.deviceType, IsConnected: "1"}
				h.broadcastToAdmin <- &serverMesg

				// else it is a status message
			} else {
				serverMesg := serverMessage{ClientId: message.ClientId, Status: message.Status}
				h.broadcastToAdmin <- &serverMesg
			}
		} else {
			var message actionOnPhone
			if err := c.socket.ReadJSON(&message); err != nil {
				log.Println(err)
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
			connectedPhones[key.id] = &serverMessage{ClientId: key.id, Device: key.deviceType, IsConnected: "1"}
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

func processLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		log.Println(r)
		http.Error(w, "Method not allowed", 405)
		return
	}
	file, _ /*header*/, err := r.FormFile("logfile")
	if err != nil {
		log.Println(err)
		http.Error(w, "Bad Request: problem with logfile", 400)
		return
	}
	defer file.Close()

	clientId := r.FormValue("client_id")
	if clientId == "" {
		http.Error(w, "Bad Request: no client_id present", 400)
		return
	}
	fileName := clientId + "_" + time.Now().Local().Format(time.StampMilli) + ".csv"
	// -1 below: replace all occurrences
	fileName = strings.Replace(fileName, ":", "", -1)
	outFile, err := os.Create(fileName)
	if err != nil {
		log.Println("Problem creating file, check access, space")
		http.Error(w, "Problem creating log file", 500)
		return
	}
	defer outFile.Close()
	// write from POST to outFile
	_, err = io.Copy(outFile, file)
	if err != nil {
		log.Println("Problem storing file, check space")
		http.Error(w, "Problem storing log file", 500)
		return
	}
	log.Println("Successfully stored file " + outFile.Name())
	h.broadcastToAdmin <- &serverMessage{ClientId: clientId, Status: "LOG"}
	w.WriteHeader(200)

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
	http.HandleFunc("/log", processLog)
	log.Printf("Running on port %d\n", *port)
	address := fmt.Sprintf(":%d", *port)
	err := http.ListenAndServe(address, nil)
	fmt.Println(err.Error())
}
