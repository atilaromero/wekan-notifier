package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/globalsign/mgo"
	"github.com/globalsign/mgo/bson"
	"github.com/gorilla/mux"
)

func main() {
	port, ok := os.LookupEnv("PORT")
	if !ok {
		port = "80"
	}
	mongoURL, ok := os.LookupEnv("MONGO_URL")
	if !ok {
		log.Fatalf("MONGO_URL not set. Example: MONGO_URL=mongodb://myserver:27017")
		return
	}
	mongoDatabase, ok := os.LookupEnv("MONGO_DATABASE")
	if !ok {
		mongoDatabase = "sard"
	}
	mongoCollection, ok := os.LookupEnv("MONGO_COLLECTION")
	if !ok {
		mongoCollection = "material"
	}

	client, err := mgo.Dial(mongoURL)
	if err != nil {
		log.Fatalf("could not connect to mongo database: %v\n", err)
		return
	}

	collection := client.DB(mongoDatabase).C(mongoCollection)

	r := mux.NewRouter()
	r.HandleFunc("/", handler(collection)).Methods("POST")

	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("could not start server: %v\n", err)
		return
	}
}

func handler(collection *mgo.Collection) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(r.Body)
		event := struct {
			Type    string `json:"type"`
			Payload struct {
				EvidencePath string `json:"evidencePath"`
			} `json:"payload"`
		}{}
		err := decoder.Decode(&event)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "error decoding request: %v\n", err)
			return
		}

		docs := make([]struct {
			ID   bson.ObjectId `bson:"_id"`
			Path string        `bson:"path"`
		}, 0)

		err = collection.Find(
			bson.M{"path": event.Payload.EvidencePath},
		).Limit(2).Select(bson.M{"path": 1}).All(&docs)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "error fetching database: %v\n", err)
			return
		}
		if len(docs) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "evidence not found in database: %v\n", event.Payload.EvidencePath)
			return
		}
		if len(docs) > 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "multiple evidences found in database using same path: %v\n", event.Payload.EvidencePath)
			return
		}

		err = collection.UpdateId(docs[0].ID, bson.M{"$set": bson.M{"state": event.Type}})
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "error updating state: %v\n", err)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
