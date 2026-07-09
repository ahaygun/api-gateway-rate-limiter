// Command upstream is a tiny mock backend service used to exercise the
// gateway locally. It echoes basic request info as JSON so you can see
// which service handled a call.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	addr := flag.String("addr", ":9001", "listen address")
	name := flag.String("name", "sms-service", "service name reported in responses")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": *name,
			"method":  r.Method,
			"path":    r.URL.Path,
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	log.Printf("[%s] listening on %s", *name, *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}
