package main

import (
	"encoding/json"
	"fmt"
	"github.com/pelletier/go-toml"
	"github.com/thoj/go-ircevent"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
)

type Message struct {
	Body string `json:"body"`
}

type Events struct {
	Events []Event `json:"events"`
}

type Event struct {
	EventID   string  `json:"event_id"`
	EventType string  `json:"type"`
	Content   Content `json:"content"`
	RoomID    string  `json:"room_id"`
	UserID    string  `json:"user_id"`
}

type (
	Content map[string]interface{}
)

func register(homeserver string, as_token string, self_url string) {

	// TODO conf
	url := homeserver + "/_matrix/appservice/v1/register"

	jsonStr := `{
		"as_token":"` + as_token + `",
		"url":"` + self_url + `",
		"namespaces":{
			"aliases":[{"regex": "#meta.*", "exclusive": false}]
		}
	}`
	b := strings.NewReader(jsonStr)
	_, err := http.Post(url, "application/json", b)
	if err != nil {
		log.Fatal(err)
	} else {
		println("Registered OK")
	}

}

func ircbot(server string, channel string, port string) *irc.Connection {
	irccon1 := irc.IRC("gomatrix", "go-eventirc1")
	irccon1.VerboseCallbackHandler = true
	irccon1.Debug = true
	err := irccon1.Connect(server + ":" + port)
	if err != nil {
		log.Fatal(err.Error())
		log.Fatal("Can't connect to " + server + ".")
	}
	irccon1.AddCallback("001", func(e *irc.Event) {
		irccon1.Join(channel)
	})
	go irccon1.Loop()
	return irccon1
}

func main() {
	config, err := toml.LoadFile("config.toml")
	if err != nil {
		fmt.Println("Error:", err.Error())
		os.Exit(1)
	}

	homeserver := config.Get("matrix.homeserver").(string)
	token := config.Get("matrix.token").(string)
	bridge := config.Get("bridge.url").(string)
	// Register application service webhook on homeserver
	register(homeserver, token, bridge)

	server := config.Get("irc.server").(string)
	channel := config.Get("irc.channel").(string)
	port := config.Get("irc.port").(string)
	// Starte the central bridge bot
	bot := ircbot(server, channel, port)

	http.HandleFunc("/transactions/", func(w http.ResponseWriter, r *http.Request) {
		//tid := r.URL.Path[len("/transactions/"):]
		buf, _ := ioutil.ReadAll(r.Body)
		var events Events
		json.Unmarshal(buf, &events)
		for i := range events.Events {
			e := events.Events[i]
			switch e.EventType {
			case "m.room.message":
				fmt.Println(e.Content["body"], "content")
				fmt.Println(e.UserID, "user_id")
				bot.Privmsg(channel, "<"+e.UserID+"> "+e.Content["body"].(string))
			default:
				fmt.Println(e.EventType, "unknown event type")
			}
			println(events.Events[i].RoomID)
		}

	})

	log.Fatal(http.ListenAndServe(":9000", nil))

}
