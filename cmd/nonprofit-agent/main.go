package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/infrai-examples/nonprofit-agent-telemetry/internal/agent"
)

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	loop := agent.New(apiKey, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /runs", func(w http.ResponseWriter, r *http.Request) {
		var input agent.RunInput
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON input"})
			return
		}

		output, err := loop.Run(r.Context(), input)
		if err != nil {
			var inputErr *agent.InputError
			if errors.As(err, &inputErr) {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": inputErr.Error()})
				return
			}
			var infraiErr *agent.InfraiError
			if errors.As(err, &infraiErr) && infraiErr.StatusCode >= 400 && infraiErr.StatusCode < 500 {
				writeJSON(w, infraiErr.StatusCode, map[string]string{"error": infraiErr.Error()})
				return
			}
			log.Printf("agent run failed: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "agent run failed"})
			return
		}
		writeJSON(w, http.StatusOK, output)
	})

	log.Println("nonprofit agent listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}
