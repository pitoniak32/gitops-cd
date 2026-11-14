// promotion-log (ADR 0032): receives one JSON record per Kargo promotion from the http step that ends every
// shared ClusterPromotionTask (promote-argocd, promote-gha) and writes it to stdout as one JSON line
// ("msg":"promotion"). Alloy ships pod logs to Loki, where the CD pipeline dashboard reads them.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// maxBody caps a record; real ones are well under 2 KiB.
const maxBody = 64 << 10

// record is what the promotion step sends; unknown fields are dropped.
type record struct {
	Promotion    string `json:"promotion"`
	Project      string `json:"project"`
	System       string `json:"system"`
	App          string `json:"app"`
	Stage        string `json:"stage"`
	Target       string `json:"target"`
	Platform     string `json:"platform"`
	DeployWith   string `json:"deployWith"`
	Freight      string `json:"freight"`
	FreightAlias string `json:"freightAlias"`
	Warehouse    string `json:"warehouse"`
	Artifact     string `json:"artifact"`
	Commit       string `json:"commit"`
	Actor        string `json:"actor"`
	Automatic    bool   `json:"automatic"`
	Rollback     bool   `json:"rollback"`
	Result       string `json:"result"`
	GhaRunURL    string `json:"ghaRunURL"`
	UIURL        string `json:"uiURL"`
}

// line is one log line: the record plus msg and receivedAt.
type line struct {
	Msg        string    `json:"msg"`
	ReceivedAt time.Time `json:"receivedAt"`
	record
}

func (r record) validate() error {
	switch {
	case r.Promotion == "":
		return errors.New("promotion is required")
	case r.Project == "":
		return errors.New("project is required")
	case r.Target == "":
		return errors.New("target is required")
	case r.Result != "Succeeded" && r.Result != "Failed":
		return fmt.Errorf("result must be Succeeded or Failed, got %q", r.Result)
	}
	return nil
}

func newHandler(out io.Writer, now func() time.Time) http.Handler {
	var mu sync.Mutex
	enc := json.NewEncoder(out)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /promotions", func(w http.ResponseWriter, req *http.Request) {
		dec := json.NewDecoder(http.MaxBytesReader(w, req.Body, maxBody))
		var r record
		if err := dec.Decode(&r); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				http.Error(w, "record too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid record: "+err.Error(), http.StatusBadRequest)
			return
		}
		if dec.More() {
			http.Error(w, "invalid record: trailing data", http.StatusBadRequest)
			return
		}
		if err := r.validate(); err != nil {
			http.Error(w, "invalid record: "+err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		err := enc.Encode(line{Msg: "promotion", ReceivedAt: now().UTC(), record: r})
		mu.Unlock()
		if err != nil {
			http.Error(w, "write failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func main() {
	srv := &http.Server{
		Addr:              ":8080",
		Handler:           newHandler(os.Stdout, time.Now),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
	}
	log.SetOutput(os.Stderr)
	log.Printf("promotion-log listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}
