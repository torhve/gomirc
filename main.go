package main

import (
	"encoding/json"
	"fmt"
	"github.com/pelletier/go-toml"
	"github.com/thoj/go-ircevent"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

var hs_token HSToken
var as_token string
var channel string
var homeserver string
var homeserver_domain string
var room_id string

var bots map[string]*irc.Connection
var joins map[string]bool

type Message struct {
	Body string `json:"body"`
}

type HSToken struct {
	Token string `json:"hs_token"`
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

func register(as_token string, self_url string) {

	url := homeserver + "/_matrix/appservice/v1/register"

	jsonStr := `{
		"as_token":"` + as_token + `",
		"url":"` + self_url + `",
		"namespaces":{
			"users":[{
				"exclusive": true,
				"regex": "@irc.*"
			}],
			"aliases":[{
				"regex": "#meta.*",
				"exclusive": false}]
		}
	}`
	b := strings.NewReader(jsonStr)
	req, err := http.Post(url, "application/json", b)
	if err != nil || req.StatusCode != 200 {
		log.Fatal(err)
		os.Exit(1)
	} else {
		println("Registered OK")
	}
	buf, _ := ioutil.ReadAll(req.Body)
	json.Unmarshal(buf, &hs_token)
}

func UrlEncoded(str string) (string, error) {
	u, err := url.Parse(str)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func matrix_join(user_id string) {
	room := url.QueryEscape(room_id)
	esc_user_id := url.QueryEscape(user_id)
	url := homeserver + "/_matrix/client/api/v1" + "/rooms/" + room + "/join?access_token=" + as_token + "&user_id=" + esc_user_id
	jsonStr := "{}"
	b := strings.NewReader(jsonStr)
	req, err := http.Post(url, "", b)
	if err != nil || req.StatusCode != 200 {
		log.Fatal(err)
	}
}

func post_matrix_message(user_id string, message string) {
	room := url.QueryEscape(room_id)
	esc_user_id := url.QueryEscape(user_id)
	url := homeserver + "/_matrix/client/api/v1" + "/rooms/" + room + "/send/m.room.message?access_token=" + as_token + "&user_id=" + esc_user_id
	jsonStr := `{
		"body":"` + message + `",
		"msgtype":"m.text"
	}`
	b := strings.NewReader(jsonStr)
	_, err := http.Post(url, "application/json", b)
	if err != nil {
		log.Fatal(err)
	}
}

func check_homeserver_user(user_id string) bool {
	url := homeserver + "/_matrix/appservice/v1/users/" + user_id + "?" + hs_token.Token
	req, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
		return false
	}
	if req.StatusCode == 200 {
		return true
	} else {
		return false
	}
}

func ircbot(server string, channel string, port string, nick string, user string) *irc.Connection {
	irccon1 := irc.IRC(nick, user)
	irccon1.VerboseCallbackHandler = true
	irccon1.Debug = true
	err := irccon1.Connect(server + ":" + port)
	if err != nil {
		log.Fatal(err.Error())
		log.Fatal("Can't connect to " + server + ".")
	}
	go irccon1.Loop()
	return irccon1
}

func irc_nick_to_matrix_userid(nick string) string {
	return "@irc." + nick + ":" + homeserver_domain
}

func handle_irc_message(e *irc.Event) {
	nick := e.Nick
	//if check_homeserver_user(user_id) {
	user_id := irc_nick_to_matrix_userid(nick)
	message := e.Message()
	_, present := joins[user_id]
	// Issue a matrix join for the first time when get a message from new nick
	if !present {
		joins[user_id] = true
		matrix_join(user_id)
	}
	post_matrix_message(user_id, message)
	//}
}

func handle_matrix_message(bot *irc.Connection, event Event) {
	fmt.Println(event.Content["body"], "content")
	fmt.Println(event.UserID, "user_id")
	body := event.Content["body"].(string)
	if strings.HasPrefix(event.UserID, "@irc.") {
		return
	}
	for _, line := range strings.Split(body, "\n") {
		if len(line) > 460 { // Very safe cutoff
			line = line[0:460]
		}
		bot.Privmsgf(channel, "%s\n", line)
	}
}

func main() {
	config, err := toml.LoadFile("config.toml")
	if err != nil {
		fmt.Println("Error:", err.Error())
		os.Exit(1)
	}

	homeserver = config.Get("matrix.homeserver").(string)
	homeserver_domain = config.Get("matrix.homeserver_domain").(string)
	as_token = config.Get("matrix.token").(string)
	bridge := config.Get("bridge.url").(string)
	// Register application service webhook on homeserver
	register(as_token, bridge)

	room_id = config.Get("matrix.room_id").(string)

	server := config.Get("irc.server").(string)
	channel = config.Get("irc.channel").(string)
	port := config.Get("irc.port").(string)
	user := config.Get("irc.user").(string)
	nick := config.Get("irc.nick").(string)
	// Starte the central bridge bot
	bot := ircbot(server, channel, port, nick, user)
	bot.AddCallback("001", func(e *irc.Event) {
		bot.Join(channel)
	})
	bot.AddCallback("PRIVMSG", func(e *irc.Event) {
		// Ignore the IRC bridge itself and its own bots
		if e.Nick != nick && !strings.HasPrefix(e.Nick, "M-") {
			handle_irc_message(e)
		}
	})

	bots = make(map[string]*irc.Connection)
	joins = make(map[string]bool)

	http.HandleFunc("/transactions/", func(w http.ResponseWriter, r *http.Request) {
		//tid := r.URL.Path[len("/transactions/"):]
		buf, _ := ioutil.ReadAll(r.Body)
		var events Events
		json.Unmarshal(buf, &events)
		for _, e := range events.Events {
			// Ignore ANY event from ourself
			if strings.HasPrefix(e.UserID, "@irc.") {
				return
			}
			// ONLY listen to our roomid
			if e.RoomID != room_id {
				return
			}
			switch e.EventType {
			case "m.room.message":
				bot, present := bots[e.UserID]
				if !present {
					// TODO genereate valid nick name
					nick := "M-" + e.UserID[1:strings.Index(e.UserID, ":")]
					bot = ircbot(server, channel, port, nick, nick)
					bots[e.UserID] = bot
					bot.AddCallback("001", func(i *irc.Event) {
						bot.Join(channel)
						handle_matrix_message(bot, e)
					})
				} else {
					handle_matrix_message(bot, e)
				}
			default:
				fmt.Println(e.EventType, "unknown event type")
			}
		}

	})

	log.Fatal(http.ListenAndServe(":9000", nil))

}
