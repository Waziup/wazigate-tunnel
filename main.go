// ((( WAZIUP )))
//
// This is a Waziup project.
// The Wazigate-Tunnel is a REST over MQTT proxy what allows remote access of
// Waziup-Gateways when they connected to the Waziup Cloud.
//
// Johann Forster, 2019
//
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Waziup/waziup-tunnel/mqtt"
)

var counter int

type Downstream struct {
	body     []byte
	deviceID string
	uri      string
	method   string
	header   http.Header
	pending  chan Upstream
}

var downstream = make(chan Downstream)

type Upstream struct {
	statusCode int
	body       []byte
	header     http.Header
}

var upstream = map[int]chan Upstream{}

var upstreamMutex sync.Mutex

var client *mqtt.Client

func main() {

	go serve()

	username := os.Getenv("WAZIUP_USERNAME")
	if username == "" {
		username = "guest"
	}
	password := os.Getenv("WAZIUP_PASSWORD")
	if password == "" {
		password = "guest"
	}
	addr := os.Getenv("WAZIUP_MQTT")
	if addr == "" {
		addr = "api.waziup.io:1883"
	}

	for true {
		auth := &mqtt.ConnectAuth{
			Username: username,
			Password: password,
		}
		log.Printf("[MQTT ] Connecting to %q as %q...", addr, auth.Username)

		newClient, err := mqtt.Dial(addr, "tunnel", true, auth, nil)
		if err != nil {
			log.Printf("[MQTT ] Error %v", err)
			time.Sleep(time.Second * 3)
			continue
		}

		newClient.Subscribe("devices/+/tunnel-up/+", 0)

		log.Println("[MQTT ] Tunnel ready.")
		upstreamMutex.Lock()
		client = newClient
		upstreamMutex.Unlock()

		for true {
			msg, err := client.Message()
			if err != nil {
				log.Printf("[MQTT ] Error %v", err)
				break
			}
			if msg == nil {
				log.Printf("[MQTT ] Error Disconnected by Server.")
				break
			}
			log.Printf("[UP   ] %s s:%d", msg.Topic, len(msg.Data))

			topic := msg.Topic
			for i := 0; i < 3; i++ {
				i := strings.IndexRune(topic, '/')
				topic = topic[i+1:]
			}

			id, err := strconv.Atoi(topic)
			if err != nil {
				log.Printf("[UP   ] Error Bad Topic: %v", err)
				continue
			}

			buf := msg.Data
			l, statusCode := readInt(buf)
			if l == 0 {
				log.Printf("[UP   ] Error Bad Data.")
				continue
			}
			buf = buf[l:]
			l, b := readBytes(buf)
			if l == 0 {
				log.Printf("[UP   ] Error Bad Data.")
				continue
			}
			var header http.Header
			json.Unmarshal(b, &header)
			buf = buf[l:]
			l, body := readBytes(buf)
			if l != len(buf) {
				log.Printf("[UP   ] Error Bad Data.")
				continue
			}

			upstreamMutex.Lock()
			if stream, ok := upstream[id]; ok {
				delete(upstream, id)
				stream <- Upstream{
					statusCode: statusCode,
					body:       body,
					header:     header,
				}
				upstreamMutex.Unlock()
			} else {
				upstreamMutex.Unlock()
				log.Printf("[UP   ] Error Bad Reference.")
				continue
			}
		}

		upstreamMutex.Lock()
		client = nil
		upstreamMutex.Unlock()
		time.Sleep(time.Second * 3)
	}
}

func serve() {
	addr := os.Getenv("WAZIUP_ADDR")
	if addr == "" {
		addr = ":80"
	}
	log.Fatal(http.ListenAndServe(addr, http.HandlerFunc(handler)))
}

func handler(resp http.ResponseWriter, req *http.Request) {
	uri := req.RequestURI
	if len(uri) == 0 || uri[0] != '/' {
		http.Error(resp, "Bad Request", 400)
		return
	}
	i := strings.IndexRune(uri[1:], '/')
	if i == -1 {
		http.Error(resp, "Bad Request", 400)
		return
	}
	if req.Header.Get("Connection") == "Upgrade" {
		http.Error(resp, "Can not upgrade tunnel conenction.", http.StatusBadRequest)
		return
	}
	deviceID := uri[1 : i+1]
	req.RequestURI = uri[i+1:]
	proxy(deviceID, resp, req)
}

func proxy(deviceID string, resp http.ResponseWriter, req *http.Request) {

	body, _ := ioutil.ReadAll(req.Body)
	pending := make(chan Upstream, 1)

	var buf bytes.Buffer
	writeString(&buf, req.Method)
	writeString(&buf, req.RequestURI)
	header, _ := json.Marshal(req.Header)
	writeBytes(&buf, header)
	writeBytes(&buf, body)

	upstreamMutex.Lock()

	counter++
	if counter == 2147483647 {
		counter = 1
	}

	ref := counter

	topic := "devices/" + deviceID + "/tunnel-down/" + strconv.Itoa(ref)

	client.Publish(&mqtt.Message{
		Topic: topic,
		Data:  buf.Bytes(),
	})

	log.Printf("[DOWN ] %s s:%d", topic, buf.Len())

	upstream[ref] = pending
	upstreamMutex.Unlock()

	timeout := time.NewTimer(10 * time.Second)
	select {
	case <-timeout.C:
		http.Error(resp, "Gateway timeout.", http.StatusGatewayTimeout)
		timeout.Stop()

		upstreamMutex.Lock()
		delete(upstream, ref)
		upstreamMutex.Unlock()

	case upstream := <-pending:
		if !timeout.Stop() {
			<-timeout.C
		}
		header := resp.Header()
		for key, val := range upstream.header {
			header[key] = val
		}
		resp.WriteHeader(upstream.statusCode)
		resp.Write(upstream.body)
		log.Printf("[WWW  ] %d %s %s s:%d", upstream.statusCode, req.Method, req.RequestURI, len(upstream.body))
	}

	close(pending)
}

////////////////////////////////////////////////////////////////////////////////

func readString(buf []byte) (int, string) {
	length, b := readBytes(buf)
	return length, string(b)
}

func readBytes(buf []byte) (int, []byte) {

	if len(buf) < 2 {
		return 0, nil
	}
	length := (int(buf[0])<<16 + int(buf[1])<<8 + int(buf[2])) + 3
	if len(buf) < length {
		return 0, nil
	}
	return length, buf[3:length]
}

func writeString(w io.Writer, str string) (int, error) {
	return writeBytes(w, []byte(str))
}

func writeBytes(w io.Writer, b []byte) (int, error) {
	m, err := w.Write([]byte{byte(len(b) >> 16), byte(len(b) >> 8), byte(len(b) & 0xff)})
	if err != nil {
		return m, err
	}
	n, err := w.Write(b)
	return m + n, err
}

func writeInt(w io.Writer, i int) (int, error) {
	return w.Write([]byte{byte(i >> 8), byte(i & 0xff)})
}

func readInt(buf []byte) (int, int) {
	if len(buf) < 2 {
		return 0, 0
	}
	return 2, (int(buf[0]) << 8) + int(buf[1])
}
