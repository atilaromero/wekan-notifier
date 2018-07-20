package main

import (
	"encoding/json"
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
	}

	collection := client.DB(mongoDatabase).C(mongoCollection)

	r := mux.NewRouter()
	r.HandleFunc("/", handler(collection)).Methods("POST")

	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("could not start server: %v\n", err)
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
			log.Fatalf("error decoding request: %v\n", err)
		}

		docs := make([]struct {
			ID   bson.ObjectId `bson:"_id"`
			Path string        `bson:"path"`
		}, 0)

		err = collection.Find(
			bson.M{"path": event.Payload.EvidencePath},
		).Limit(2).Select(bson.M{"path": 1}).All(&docs)
		if err != nil {
			log.Fatalf("error fetching database: %v\n", err)
		}
		if len(docs) == 0 {
			log.Fatalf("evidence not found in database: %v\n", event.Payload.EvidencePath)
		}
		if len(docs) > 1 {
			log.Fatalf("multiple evidences found in database using same path: %v\n", event.Payload.EvidencePath)
		}

		collection.UpdateId(docs[0].ID, bson.M{"$set": bson.M{"state": event.Type}})
		w.WriteHeader(http.StatusOK)
	}
}
