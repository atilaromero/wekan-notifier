package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
)

type config struct {
	port       string
	graphqlURL string
	user       string
	pass       string
	userID     string
	token      string
	list       string
	board      string
}

func main() {
	port, ok := os.LookupEnv("PORT")
	if !ok {
		port = "80"
	}
	graphqlURL, ok := os.LookupEnv("GRAPHQL_URL")
	if !ok {
		log.Fatalf("GRAPHQL_URL not set. Example: GRAPHQL_URL=http://myserver:80")
		return
	}
	user, ok := os.LookupEnv("USER")
	if !ok {
		log.Fatalf("Graphql USER not set.")
		return
	}
	pass, ok := os.LookupEnv("PASS")
	if !ok {
		log.Fatalf("Graphql PASS not set.")
		return
	}
	list, ok := os.LookupEnv("LIST")
	if !ok {
		log.Fatalf("Wekan LIST not set.")
		return
	}
	board, ok := os.LookupEnv("BOARD")
	if !ok {
		log.Fatalf("Wekan BOARD not set.")
		return
	}

	cnf := config{
		port:       port,
		graphqlURL: graphqlURL,
		user:       user,
		pass:       pass,
		list:       list,
		board:      board,
	}

	if cnf.token == "" {
		err := cnf.getToken()
		if err != nil {
			log.Fatalf("error in getToken: %v", err)
			return
		}
	}

	r := mux.NewRouter()
	r.HandleFunc("/", handler(&cnf)).Methods("POST")

	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("could not start server: %v\n", err)
		return
	}
}

type Event struct {
	Type    string `json:"type"`
	Payload struct {
		EvidencePath string `json:"evidencePath"`
		Progress     string `json:"progress"`
	} `json:"payload"`
}

type Card struct {
	ID           string
	CustomFields []struct {
		ID    string
		Name  string
		Value string
	}
}

func handler(cnf *config) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(r.Body)
		event := Event{}
		err := decoder.Decode(&event)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "error decoding request: %v\n", err)
			log.Printf("error decoding request: %v\n", err)
			return
		}
		card, err := findCard(cnf, event.Payload.EvidencePath)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "error findind card: %v\n", err)
			log.Printf("error findind card: %v\n", err)
			return
		}

		switch event.Type {
		case "running", "done", "failed":
			err = updateStatus(cnf, card, event.Type)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, "error updating state: %v\n", err)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "progress":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "unexpected type: %v\n", event.Type)
		}
	}
}

func updateStatus(cnf *config, card Card, status string) error {
	fields := ""
	for _, field := range card.CustomFields {
		if field.Name == "status" {
			field.Value = status
		}
		fields += fmt.Sprintf("{_id:\"%s\", value:\"%s\"},", field.ID, field.Value)
	}
	q := fmt.Sprintf(`
	mutation{
		updateCard(auth:{userId:"%s", token:"%s"},
			boardTitle:"%s",
			listTitle:"%s",
			card:{
				_id: "%s",
				customFields: [%s]
			}
		)
	}
	`, cnf.userID, cnf.token, cnf.board, cnf.list, card.ID, fields)
	d := struct {
		Errors []struct {
			Message string
		}
		Data struct {
			UpdateCard string
		}
	}{}
	err := cnf.query(q, &d)
	if err != nil {
		return err
	}
	if len(d.Errors) > 0 {
		return fmt.Errorf(d.Errors[0].Message)
	}
	return nil
}

func findCard(cnf *config, path string) (Card, error) {
	if cnf.token == "" {
		err := cnf.getToken()
		if err != nil {
			return Card{}, fmt.Errorf("error in getToken: %v", err)
		}
	}
	q := fmt.Sprintf(`
	query{
		board(auth:{userId:"%s", token:"%s"}, title:"%s"){
			customFields{
				id: _id
				name
			}
			list(title:"%s"){
				cards{
					id:_id
					customFields{
						id: _id
						value
					}
				}
			}
		}
	}
	`, cnf.userID, cnf.token, cnf.board, cnf.list)
	d := struct {
		Errors []struct {
			Message string
		}
		Data struct {
			Board struct {
				CustomFields []struct {
					ID   string
					Name string
				}
				List struct {
					Cards []Card
				}
			}
		}
	}{}
	err := cnf.query(q, &d)
	if err != nil {
		return Card{}, err
	}
	if len(d.Errors) > 0 {
		return Card{}, fmt.Errorf(d.Errors[0].Message)
	}
	fieldNames := make(map[string]string)
	for _, x := range d.Data.Board.CustomFields {
		fieldNames[x.Name] = x.ID
	}
	fieldIDs := make(map[string]string)
	for _, x := range d.Data.Board.CustomFields {
		fieldIDs[x.ID] = x.Name
	}
	for _, card := range d.Data.Board.List.Cards {
		for i := range card.CustomFields {
			card.CustomFields[i].Name = fieldIDs[card.CustomFields[i].ID]
		}
		for _, field := range card.CustomFields {
			if field.Name == "path" {
				if field.Value == path {
					return card, nil
				}
			}
		}
	}
	return Card{}, fmt.Errorf("path not found: %s", path)
}

func (cnf *config) getToken() error {
	q := fmt.Sprintf(`
	query{
		authorize(user:"%s", password:"%s"){
			userId
			token
		}
	}
	`, cnf.user, cnf.pass)
	d := struct {
		Errors []struct {
			Message string
		}
		Data struct {
			Authorize struct {
				UserId string
				Token  string
			}
		}
	}{}
	err := cnf.query(q, &d)
	if err != nil {
		return err
	}
	if len(d.Errors) > 0 {
		return fmt.Errorf(d.Errors[0].Message)
	}
	cnf.userID = d.Data.Authorize.UserId
	cnf.token = d.Data.Authorize.Token
	return nil
}

func (cnf *config) query(q string, v interface{}) error {
	r, err := http.Post(cnf.graphqlURL, "application/graphql", strings.NewReader(q))
	if err != nil {
		return err
	}
	defer r.Body.Close()
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}
	err = json.Unmarshal(b, v)
	if err != nil {
		return err
	}
	return nil
}
