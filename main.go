// ((( WAZIUP )))
//
// This is a Waziup project.
// The Wazigate-Tunnel is a REST over MQTT proxy what allows remote access of
// Waziup-Gateways when they connected to the Waziup Cloud.
//
// Johann Forster, 2019
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Waziup/waziup-tunnel/mqtt"
	"golang.org/x/oauth2"
)

var counter int

var CookieName = "WaziupTunnelToken"

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

		id := "tunnel-" + fmt.Sprintf("%f", rand.Float32())[2:]
		log.Printf("[MQTT ] Connecting %q to %q as %q...", id, addr, auth.Username)
		newClient, err := mqtt.Dial(addr, id, true, auth, nil)
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
	log.Printf("Listening on %q.", addr)
	log.Fatal(http.ListenAndServe(addr, http.HandlerFunc(handler)))
}

var www = http.FileServer(http.Dir("www"))

var clientID, ClientSecret, RedirectURL, TokenURL, AuthURL string

func handler(resp http.ResponseWriter, req *http.Request) {

	if clientID == "" {
		clientID = os.Getenv("WAZIUP_CLIENTID")
		ClientSecret = os.Getenv("WAZIUP_CLIENTSECRET")
		RedirectURL = os.Getenv("WAZIUP_REDIRECTURL")
		TokenURL = os.Getenv("WAZIUP_TOKENURL")
		AuthURL = os.Getenv("WAZIUP_AUTHURL")
	}

	i := strings.IndexRune(req.URL.Path[1:], '/')
	if i == -1 {
		if req.URL.Path == "/gateways.json" {
			serveGateways(resp, req)
			return
		}
		www.ServeHTTP(resp, req)
		return
	}

	gatewayID := req.URL.Path[1 : i+1]

	oAuthConfig := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: ClientSecret,
		RedirectURL:  RedirectURL + req.URL.Path,

		Scopes: []string{},
		Endpoint: oauth2.Endpoint{
			TokenURL: TokenURL,
			AuthURL:  AuthURL,
		},
	}

	query := req.URL.Query()
	sessState := query.Get("state")
	if sessState == "state" {
		sessCode := query.Get("code")

		ctx := context.Background()
		token, err := oAuthConfig.Exchange(ctx, sessCode)
		if err == nil {
			cookie := http.Cookie{
				Name:  CookieName,
				Value: token.AccessToken,
				Path:  "/",
			}
			http.SetCookie(resp, &cookie)
			// resp.Header().Set("Set-Cookie", CookieName+"="+token.AccessToken)
			gateways, err := FetchGateways(token.AccessToken)
			if err != nil {
				http.Error(resp, err.Error(), http.StatusInternalServerError)
				return
			}
			sess := CreateSession(token.AccessToken)
			sess.gateways = gateways
			http.Redirect(resp, req, req.URL.Path, http.StatusSeeOther)
			return
		}

		http.Error(resp, err.Error(), http.StatusInternalServerError)
		return
	}

	sessToken, err := req.Cookie(CookieName)
	if err == nil {
		sess := sessions[sessToken.Value]
		if sess != nil {
			perm := sess.gateways[gatewayID]
			if perm == 0 {
				http.Error(resp, "Not allowed to access this gateway.", http.StatusForbidden)
				return
			}
			reqPerm := 0
			switch req.Method {
			case http.MethodPost, http.MethodPut, http.MethodDelete:
				reqPerm |= PermUpdate
			case http.MethodHead, http.MethodGet:
				reqPerm |= PermView
			}

			if perm&reqPerm == 0 {
				http.Error(resp, "Method not allowed.", http.StatusMethodNotAllowed)
				return
			}

			if req.Header.Get("Connection") == "Upgrade" {
				http.Error(resp, "Can not upgrade tunnel conenction.", http.StatusBadRequest)
				return
			}

			req.RequestURI = req.RequestURI[i+1:]
			proxy(gatewayID, resp, req)
			return
		}
	}

	// oAuthConfig.RedirectURL = "http://localhost:8080/?forward=" + url.QueryEscape(req.URL.Path)
	url := oAuthConfig.AuthCodeURL("state", oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("response_mode", "query"))
	http.Redirect(resp, req, url, http.StatusSeeOther)
}

const (
	// PermView is required to view gateways.
	PermView = 1 << iota
	// PermUpdate is required to update (manage) gateways.
	PermUpdate
	// PermDelete is required to delete gateways. (Not used right now.)
	PermDelete
)

// FetchGateways lists all gateways and permissions for this token.
func FetchGateways(token string) (map[string]int, error) {

	auth := "Bearer " + token
	resp := fetch("https://api.waziup.io/api/v2/auth/permissions/gateways", fetchInit{
		method: http.MethodGet,
		headers: map[string]string{
			"Authorization": auth,
		},
	})
	if !resp.ok {
		return nil, fmt.Errorf("fetch %d: %s", resp.status, resp.statusText)
	}

	var body []struct {
		Scopes   []string `json:"scopes"`
		Resource string   `json:"resource"`
	}

	if err := resp.json(&body); err != nil {
		return nil, fmt.Errorf("fetch decode: %s", err.Error())
	}

	gws := make(map[string]int, len(body))

	for _, gw := range body {
		perm := 0
		for _, scope := range gw.Scopes {
			switch scope {
			case "gateways:view":
				perm |= PermView
			case "gateways:update":
				perm |= PermUpdate
			case "gateways:delete":
				perm |= PermDelete
			}
		}
		if perm != 0 {
			gws[gw.Resource] = perm
		}
	}

	return gws, nil
}

func serveGateways(resp http.ResponseWriter, req *http.Request) {

	sessToken, err := req.Cookie(CookieName)
	if err != nil {
		http.Error(resp, "login required", http.StatusUnauthorized)
		return
	}
	sess := sessions[sessToken.Value]
	if sess == nil {
		http.Error(resp, "no session with that id", http.StatusNotFound)
		return
	}
	resp.Header().Set("Content-Type", "application/json; charset=utf-8")
	encoder := json.NewEncoder(resp)
	encoder.Encode(sess.gateways)
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

	timeout := time.NewTimer(120 * time.Second)
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
